
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>
#include <sqlite3.h>
#ifdef DEX_HAS_POSTGRES
#include <libpq-fe.h>
#endif
#ifdef DEX_HAS_MYSQL
#include <mysql/mysql.h>
#endif
#ifdef DEX_HAS_MONGO
#include <mongoc/mongoc.h>
#endif

#define DEX_DB_MAX_CONNS 16
#define DEX_DB_MAX_RESULTS 64
#define DEX_DB_MAX_STMTS 64
#define DEX_DB_POOL_SIZE 16

#define DEX_DB_DRIVER_NONE 0
#define DEX_DB_DRIVER_SQLITE 1
#define DEX_DB_DRIVER_POSTGRES 2
#define DEX_DB_DRIVER_MYSQL 3
#define DEX_DB_DRIVER_MONGO 4

#ifdef DEX_HAS_POSTGRES
typedef struct {
    PGconn* connections[DEX_DB_POOL_SIZE];
    int in_use[DEX_DB_POOL_SIZE];    // 0 = free, 1 = acquired
    // Bumped whenever an entry's socket is replaced, so anything cached against
    // a connection — a server-side prepared statement — knows it is stale.
    unsigned int generation[DEX_DB_POOL_SIZE];
    int size;                         // actual number of connections
    // Kept so a dead entry can be rebuilt from scratch when PQreset cannot
    // revive it.
    char dsn[1024];
    pthread_mutex_t lock;
    pthread_cond_t available;
} DexDbPool;
#endif

#ifdef DEX_HAS_MONGO
static int dex_mongo_initialized = 0;
#endif

typedef struct {
    int driver;
    sqlite3* sqlite_conn;
#ifdef DEX_HAS_POSTGRES
    DexDbPool pg_pool;
#endif
#ifdef DEX_HAS_MYSQL
    MYSQL* mysql_conn;
#endif
#ifdef DEX_HAS_MONGO
    mongoc_client_t* mongo_client;
    char mongo_dbname[256];
#endif
} DexDbConn;

typedef struct {
    int driver;
    // SQLite
    sqlite3_stmt* sqlite_stmt;
    int sqlite_done;
#ifdef DEX_HAS_POSTGRES
    // Postgres
    PGresult* pg_result;
    int pg_row;
    int pg_nrows;
    int pool_entry;   // index into the pool's connections array
    int pool_conn;    // which DexDbConn slot this came from
#endif
#ifdef DEX_HAS_MYSQL
    // MySQL
    MYSQL_RES* mysql_result;
    MYSQL_ROW mysql_row;
#endif
#ifdef DEX_HAS_MONGO
    // MongoDB
    mongoc_cursor_t* mongo_cursor;
    const bson_t* mongo_doc;
#endif
} DexDbResult;

typedef struct {
    int driver;
    int conn_slot; // which connection this stmt belongs to
    sqlite3_stmt* sqlite_stmt;
#ifdef DEX_HAS_POSTGRES
    // Which pool connections carry this statement, and at which generation. 0
    // means "not prepared there"; a generation mismatch means the socket was
    // replaced underneath it and it has to be prepared again.
    unsigned int prepared_gen[DEX_DB_POOL_SIZE];
    char* pg_sql;
    char pg_stmt_name[48];
    int pg_param_count;
    int pg_param_cap;
    char** pg_param_values;
    int* pg_param_lengths;
    PGresult* pg_result;
    int pg_current_row;
    int pg_num_rows;
#endif
#ifdef DEX_HAS_MYSQL
    MYSQL_STMT* mysql_stmt;
#endif
} DexDbStmt;

static DexDbConn dex_db_conns[DEX_DB_MAX_CONNS];
static DexDbResult dex_db_results[DEX_DB_MAX_RESULTS];
static DexDbStmt dex_db_stmts[DEX_DB_MAX_STMTS];
static pthread_mutex_t dex_db_mutex = PTHREAD_MUTEX_INITIALIZER;

#ifdef DEX_HAS_POSTGRES
// Names are never reused, even after a statement slot is recycled: a stale
// prepared statement left on a connection must not collide with a new one.
static unsigned int dex_db_stmt_seq = 0;

// Bring one pool entry back to a usable state. The caller holds the entry
// (in_use is set) but must NOT hold the pool lock: reconnecting blocks, and
// every other thread would block behind it.
// How many connections a pool opens. Sixteen per process is generous on one
// host and ruinous across a dozen: Postgres spends max_connections across every
// client at once, so a fleet needs this turned down or a pooler in front.
static int dex_db_pool_size(void) {
    const char* env = getenv("DB_POOL_SIZE");
    if (!env || !*env) return DEX_DB_POOL_SIZE;
    int n = atoi(env);
    if (n < 1) n = 1;
    if (n > DEX_DB_POOL_SIZE) n = DEX_DB_POOL_SIZE;
    return n;
}

static int dex_db_pg_revive(DexDbPool* pool, int entry) {
    PGconn* pg = pool->connections[entry];
    if (pg && PQstatus(pg) == CONNECTION_OK) {
        return 1;
    }

    // PQreset reuses the parameters the connection was opened with and keeps
    // the same PGconn, so it is the cheap path back.
    if (pg) {
        PQreset(pg);
        if (PQstatus(pg) == CONNECTION_OK) {
            pool->generation[entry]++;
            return 1;
        }
        PQfinish(pg);
        pool->connections[entry] = NULL;
    }

    pg = PQconnectdb(pool->dsn);
    if (PQstatus(pg) != CONNECTION_OK) {
        fprintf(stderr, "db: reconnect failed: %s", PQerrorMessage(pg));
        PQfinish(pg);
        pool->connections[entry] = NULL;
        return 0;
    }
    pool->connections[entry] = pg;
    pool->generation[entry]++;
    return 1;
}

// Returns a pool index whose connection has just been checked, or -1 when the
// database cannot be reached at all. Blocks while every entry is busy.
static int dex_db_pool_acquire(DexDbPool* pool) {
    pthread_mutex_lock(&pool->lock);
    int entry = -1;
    while (entry < 0) {
        for (int i = 0; i < pool->size; i++) {
            if (!pool->in_use[i]) {
                pool->in_use[i] = 1;
                entry = i;
                break;
            }
        }
        if (entry < 0) {
            pthread_cond_wait(&pool->available, &pool->lock);
        }
    }
    pthread_mutex_unlock(&pool->lock);

    if (!dex_db_pg_revive(pool, entry)) {
        pthread_mutex_lock(&pool->lock);
        pool->in_use[entry] = 0;
        pthread_cond_signal(&pool->available);
        pthread_mutex_unlock(&pool->lock);
        return -1;
    }
    return entry;
}

// Waits for one particular entry rather than whichever comes free. finalize()
// needs to visit every connection that carries a statement, which acquiring
// "any" entry cannot guarantee — it can hand back the same one every time.
static int dex_db_pool_acquire_entry(DexDbPool* pool, int entry) {
    if (entry < 0 || entry >= pool->size) return 0;
    pthread_mutex_lock(&pool->lock);
    while (pool->in_use[entry]) {
        pthread_cond_wait(&pool->available, &pool->lock);
    }
    pool->in_use[entry] = 1;
    pthread_mutex_unlock(&pool->lock);
    return 1;
}

static void dex_db_pool_release(DexDbPool* pool, int entry) {
    if (entry < 0) return;
    pthread_mutex_lock(&pool->lock);
    pool->in_use[entry] = 0;
    pthread_cond_signal(&pool->available);
    pthread_mutex_unlock(&pool->lock);
}

// Distinguishes a statement Postgres rejected — which leaves the connection
// perfectly usable — from a connection that died under it. Only the second is
// worth retrying.
static int dex_db_pg_broken(PGconn* pg) {
    return pg == NULL || PQstatus(pg) != CONNECTION_OK;
}
#endif

#ifdef DEX_HAS_MYSQL
// --- MySQL DSN parser ---
// Format: "host=localhost user=root password=secret dbname=mydb port=3306"
static void dex_db_parse_mysql_dsn(const char* dsn,
    char* host, char* user, char* password, char* dbname, unsigned int* port) {
    strcpy(host, "localhost");
    strcpy(user, "root");
    password[0] = '\0';
    dbname[0] = '\0';
    *port = 3306;

    const char* p = dsn;
    while (*p) {
        while (*p == ' ') p++;
        if (!*p) break;

        const char* key_start = p;
        while (*p && *p != '=') p++;
        if (!*p) break;
        size_t key_len = (size_t)(p - key_start);
        p++;

        const char* val_start = p;
        while (*p && *p != ' ') p++;
        size_t val_len = (size_t)(p - val_start);

        if (key_len == 4 && strncmp(key_start, "host", 4) == 0) {
            strncpy(host, val_start, val_len); host[val_len] = '\0';
        } else if (key_len == 4 && strncmp(key_start, "user", 4) == 0) {
            strncpy(user, val_start, val_len); user[val_len] = '\0';
        } else if (key_len == 8 && strncmp(key_start, "password", 8) == 0) {
            strncpy(password, val_start, val_len); password[val_len] = '\0';
        } else if (key_len == 6 && strncmp(key_start, "dbname", 6) == 0) {
            strncpy(dbname, val_start, val_len); dbname[val_len] = '\0';
        } else if (key_len == 4 && strncmp(key_start, "port", 4) == 0) {
            char port_str[16];
            if (val_len > 15) val_len = 15;
            strncpy(port_str, val_start, val_len); port_str[val_len] = '\0';
            *port = (unsigned int)atoi(port_str);
        }
    }
}
#endif

#ifdef DEX_HAS_MONGO
// --- MongoDB BSON field access by column index ---
static _Bool dex_db_mongo_iter_to(const bson_t* doc, int col, bson_iter_t* iter) {
    if (!doc) return 0;
    bson_iter_init(iter, doc);
    for (int i = 0; i <= col; i++) {
        if (!bson_iter_next(iter)) return 0;
    }
    return 1;
}
#endif

// ==========================================================================
// dex_db_open
// ==========================================================================
int dex_db_open(const char* driver, const char* dsn) {
    pthread_mutex_lock(&dex_db_mutex);
    int slot = -1;
    for (int i = 0; i < DEX_DB_MAX_CONNS; i++) {
        if (dex_db_conns[i].driver == DEX_DB_DRIVER_NONE) {
            slot = i;
            dex_db_conns[i].driver = -1; // reserve slot
            break;
        }
    }
    pthread_mutex_unlock(&dex_db_mutex);
    if (slot < 0) return -1;

    if (strcmp(driver, "sqlite") == 0) {
        sqlite3* db = NULL;
        int rc = sqlite3_open(dsn, &db);
        if (rc != SQLITE_OK) {
            if (db) sqlite3_close(db);
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_conns[slot], 0, sizeof(DexDbConn));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        pthread_mutex_lock(&dex_db_mutex);
        dex_db_conns[slot].driver = DEX_DB_DRIVER_SQLITE;
        dex_db_conns[slot].sqlite_conn = db;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;

#ifdef DEX_HAS_POSTGRES
    } else if (strcmp(driver, "postgres") == 0) {
        DexDbPool* pool = &dex_db_conns[slot].pg_pool;
        memset(pool, 0, sizeof(DexDbPool));
        pthread_mutex_init(&pool->lock, NULL);
        pthread_cond_init(&pool->available, NULL);
        strncpy(pool->dsn, dsn, sizeof(pool->dsn) - 1);
        pool->dsn[sizeof(pool->dsn) - 1] = '\0';
        int want = dex_db_pool_size();
        for (int i = 0; i < want; i++) {
            PGconn* pg = PQconnectdb(dsn);
            if (PQstatus(pg) != CONNECTION_OK) {
                PQfinish(pg);
                // Close any connections already opened
                for (int j = 0; j < i; j++) {
                    PQfinish(pool->connections[j]);
                }
                pthread_mutex_destroy(&pool->lock);
                pthread_cond_destroy(&pool->available);
                pthread_mutex_lock(&dex_db_mutex);
                memset(&dex_db_conns[slot], 0, sizeof(DexDbConn));
                pthread_mutex_unlock(&dex_db_mutex);
                return -1;
            }
            pool->connections[i] = pg;
            pool->in_use[i] = 0;
            // Generations count from 1 so that 0 can mean "never prepared here".
            pool->generation[i] = 1;
        }
        pool->size = want;
        pthread_mutex_lock(&dex_db_mutex);
        dex_db_conns[slot].driver = DEX_DB_DRIVER_POSTGRES;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;
#endif

#ifdef DEX_HAS_MYSQL
    } else if (strcmp(driver, "mysql") == 0) {
        MYSQL* my = mysql_init(NULL);
        if (!my) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_conns[slot], 0, sizeof(DexDbConn));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }

        char host[256], user[256], password[256], dbname[256];
        unsigned int port = 3306;
        dex_db_parse_mysql_dsn(dsn, host, user, password, dbname, &port);

        const char* db_param = dbname[0] ? dbname : NULL;
        const char* pass_param = password[0] ? password : NULL;

        if (!mysql_real_connect(my, host, user, pass_param, db_param, port, NULL, 0)) {
            mysql_close(my);
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_conns[slot], 0, sizeof(DexDbConn));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        pthread_mutex_lock(&dex_db_mutex);
        dex_db_conns[slot].driver = DEX_DB_DRIVER_MYSQL;
        dex_db_conns[slot].mysql_conn = my;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;
#endif

#ifdef DEX_HAS_MONGO
    } else if (strcmp(driver, "mongo") == 0) {
        if (!dex_mongo_initialized) {
            mongoc_init();
            dex_mongo_initialized = 1;
        }
        mongoc_client_t* client = mongoc_client_new(dsn);
        if (!client) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_conns[slot], 0, sizeof(DexDbConn));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }

        // Extract database name from URI
        mongoc_uri_t* uri = mongoc_uri_new(dsn);
        const char* dbname = uri ? mongoc_uri_get_database(uri) : NULL;

        pthread_mutex_lock(&dex_db_mutex);
        dex_db_conns[slot].driver = DEX_DB_DRIVER_MONGO;
        dex_db_conns[slot].mongo_client = client;
        if (dbname && dbname[0]) {
            strncpy(dex_db_conns[slot].mongo_dbname, dbname, 255);
            dex_db_conns[slot].mongo_dbname[255] = '\0';
        } else {
            strcpy(dex_db_conns[slot].mongo_dbname, "test");
        }
        pthread_mutex_unlock(&dex_db_mutex);
        if (uri) mongoc_uri_destroy(uri);
        return slot;
#endif
    }
    // Unknown driver — release the reserved slot
    pthread_mutex_lock(&dex_db_mutex);
    memset(&dex_db_conns[slot], 0, sizeof(DexDbConn));
    pthread_mutex_unlock(&dex_db_mutex);
    return -1;
}

// ==========================================================================
// dex_db_exec
// ==========================================================================
int dex_db_exec(int conn, const char* sql) {
    if (conn < 0 || conn >= DEX_DB_MAX_CONNS) return -1;
    DexDbConn* c = &dex_db_conns[conn];

    if (c->driver == DEX_DB_DRIVER_SQLITE) {
        char* err = NULL;
        int rc = sqlite3_exec(c->sqlite_conn, sql, NULL, NULL, &err);
        if (err) sqlite3_free(err);
        if (rc != SQLITE_OK) return -1;
        return sqlite3_changes(c->sqlite_conn);

#ifdef DEX_HAS_POSTGRES
    } else if (c->driver == DEX_DB_DRIVER_POSTGRES) {
        DexDbPool* pool = &c->pg_pool;
        int entry = dex_db_pool_acquire(pool);
        if (entry < 0) return -1;

        // Two attempts: a connection checked out a moment ago can still die
        // before the statement lands on it, which is exactly what a database
        // restart looks like from here. A statement Postgres rejects on a
        // healthy connection is not retried — it would only fail again.
        int n = -1;
        for (int attempt = 0; attempt < 2; attempt++) {
            PGresult* res = PQexec(pool->connections[entry], sql);
            ExecStatusType status = PQresultStatus(res);
            if (status == PGRES_COMMAND_OK || status == PGRES_TUPLES_OK) {
                char* affected = PQcmdTuples(res);
                n = (affected && affected[0] != '\0') ? atoi(affected) : 0;
                PQclear(res);
                break;
            }
            int broken = dex_db_pg_broken(pool->connections[entry]);
            if (!broken) {
                fprintf(stderr, "db.exec error: %s", PQerrorMessage(pool->connections[entry]));
            }
            PQclear(res);
            if (!broken || attempt == 1) break;
            if (!dex_db_pg_revive(pool, entry)) break;
        }
        dex_db_pool_release(pool, entry);
        return n;
#endif

#ifdef DEX_HAS_MYSQL
    } else if (c->driver == DEX_DB_DRIVER_MYSQL) {
        if (mysql_query(c->mysql_conn, sql) != 0) return -1;
        MYSQL_RES* res = mysql_store_result(c->mysql_conn);
        if (res) mysql_free_result(res);
        return (int)mysql_affected_rows(c->mysql_conn);
#endif

#ifdef DEX_HAS_MONGO
    } else if (c->driver == DEX_DB_DRIVER_MONGO) {
        bson_error_t error;
        bson_t* command = bson_new_from_json((const uint8_t*)sql, -1, &error);
        if (!command) return -1;

        bson_t reply;
        mongoc_database_t* db = mongoc_client_get_database(c->mongo_client, c->mongo_dbname);
        _Bool ok = mongoc_database_command_simple(db, command, NULL, &reply, &error);

        int n = 0;
        if (ok) {
            bson_iter_t iter;
            if (bson_iter_init_find(&iter, &reply, "n")) {
                n = bson_iter_int32(&iter);
            }
        }
        bson_destroy(command);
        bson_destroy(&reply);
        mongoc_database_destroy(db);
        return ok ? n : -1;
#endif
    }
    return -1;
}

// ==========================================================================
// dex_db_query
// ==========================================================================
int dex_db_query(int conn, const char* sql) {
    if (conn < 0 || conn >= DEX_DB_MAX_CONNS) return -1;
    DexDbConn* c = &dex_db_conns[conn];

    pthread_mutex_lock(&dex_db_mutex);
    int slot = -1;
    for (int i = 0; i < DEX_DB_MAX_RESULTS; i++) {
        if (dex_db_results[i].driver == DEX_DB_DRIVER_NONE) {
            slot = i;
            dex_db_results[i].driver = -1; // reserve slot
            break;
        }
    }
    pthread_mutex_unlock(&dex_db_mutex);
    if (slot < 0) return -1;

    if (c->driver == DEX_DB_DRIVER_SQLITE) {
        sqlite3_stmt* stmt = NULL;
        int rc = sqlite3_prepare_v2(c->sqlite_conn, sql, -1, &stmt, NULL);
        if (rc != SQLITE_OK) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_results[slot], 0, sizeof(DexDbResult));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        pthread_mutex_lock(&dex_db_mutex);
        dex_db_results[slot].driver = DEX_DB_DRIVER_SQLITE;
        dex_db_results[slot].sqlite_stmt = stmt;
        dex_db_results[slot].sqlite_done = 0;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;

#ifdef DEX_HAS_POSTGRES
    } else if (c->driver == DEX_DB_DRIVER_POSTGRES) {
        DexDbPool* pool = &c->pg_pool;
        int entry = dex_db_pool_acquire(pool);
        if (entry < 0) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_results[slot], 0, sizeof(DexDbResult));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }

        PGresult* res = NULL;
        for (int attempt = 0; attempt < 2; attempt++) {
            res = PQexec(pool->connections[entry], sql);
            if (PQresultStatus(res) == PGRES_TUPLES_OK) break;
            int broken = dex_db_pg_broken(pool->connections[entry]);
            if (!broken) {
                fprintf(stderr, "db.query error: %s", PQerrorMessage(pool->connections[entry]));
            }
            PQclear(res);
            res = NULL;
            if (!broken || attempt == 1) break;
            if (!dex_db_pg_revive(pool, entry)) break;
        }

        // A PGresult owns its rows outright, so the connection goes back now
        // rather than at db.free(). A caller who forgets to free leaks one
        // result slot instead of permanently costing the pool a connection.
        dex_db_pool_release(pool, entry);

        if (!res) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_results[slot], 0, sizeof(DexDbResult));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }

        pthread_mutex_lock(&dex_db_mutex);
        dex_db_results[slot].driver = DEX_DB_DRIVER_POSTGRES;
        dex_db_results[slot].pg_result = res;
        dex_db_results[slot].pg_row = -1;
        dex_db_results[slot].pg_nrows = PQntuples(res);
        dex_db_results[slot].pool_entry = -1;
        dex_db_results[slot].pool_conn = -1;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;
#endif

#ifdef DEX_HAS_MYSQL
    } else if (c->driver == DEX_DB_DRIVER_MYSQL) {
        if (mysql_query(c->mysql_conn, sql) != 0) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_results[slot], 0, sizeof(DexDbResult));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        MYSQL_RES* res = mysql_store_result(c->mysql_conn);
        if (!res) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_results[slot], 0, sizeof(DexDbResult));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        pthread_mutex_lock(&dex_db_mutex);
        dex_db_results[slot].driver = DEX_DB_DRIVER_MYSQL;
        dex_db_results[slot].mysql_result = res;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;
#endif

#ifdef DEX_HAS_MONGO
    } else if (c->driver == DEX_DB_DRIVER_MONGO) {
        // sql parameter is the collection name
        mongoc_collection_t* coll = mongoc_client_get_collection(
            c->mongo_client, c->mongo_dbname, sql);
        bson_t* filter = bson_new();
        mongoc_cursor_t* cursor = mongoc_collection_find_with_opts(coll, filter, NULL, NULL);
        bson_destroy(filter);
        mongoc_collection_destroy(coll);

        pthread_mutex_lock(&dex_db_mutex);
        dex_db_results[slot].driver = DEX_DB_DRIVER_MONGO;
        dex_db_results[slot].mongo_cursor = cursor;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;
#endif
    }
    // Unknown driver — release the reserved slot
    pthread_mutex_lock(&dex_db_mutex);
    memset(&dex_db_results[slot], 0, sizeof(DexDbResult));
    pthread_mutex_unlock(&dex_db_mutex);
    return -1;
}

// ==========================================================================
// dex_db_next
// ==========================================================================
_Bool dex_db_next(int rows) {
    if (rows < 0 || rows >= DEX_DB_MAX_RESULTS) return 0;
    DexDbResult* r = &dex_db_results[rows];

    if (r->driver == DEX_DB_DRIVER_SQLITE) {
        if (r->sqlite_done) return 0;
        int rc = sqlite3_step(r->sqlite_stmt);
        if (rc == SQLITE_ROW) return 1;
        r->sqlite_done = 1;
        return 0;

#ifdef DEX_HAS_POSTGRES
    } else if (r->driver == DEX_DB_DRIVER_POSTGRES) {
        r->pg_row++;
        return r->pg_row < r->pg_nrows;
#endif

#ifdef DEX_HAS_MYSQL
    } else if (r->driver == DEX_DB_DRIVER_MYSQL) {
        r->mysql_row = mysql_fetch_row(r->mysql_result);
        return r->mysql_row != NULL;
#endif

#ifdef DEX_HAS_MONGO
    } else if (r->driver == DEX_DB_DRIVER_MONGO) {
        return mongoc_cursor_next(r->mongo_cursor, &r->mongo_doc);
#endif
    }
    return 0;
}

// ==========================================================================
// dex_db_col_int
// ==========================================================================
int dex_db_col_int(int rows, int col) {
    if (rows < 0 || rows >= DEX_DB_MAX_RESULTS) return 0;
    DexDbResult* r = &dex_db_results[rows];

    if (r->driver == DEX_DB_DRIVER_SQLITE) {
        return sqlite3_column_int(r->sqlite_stmt, col);

#ifdef DEX_HAS_POSTGRES
    } else if (r->driver == DEX_DB_DRIVER_POSTGRES) {
        char* val = PQgetvalue(r->pg_result, r->pg_row, col);
        if (!val || val[0] == '\0') return 0;
        return atoi(val);
#endif

#ifdef DEX_HAS_MYSQL
    } else if (r->driver == DEX_DB_DRIVER_MYSQL) {
        if (!r->mysql_row || !r->mysql_row[col]) return 0;
        return atoi(r->mysql_row[col]);
#endif

#ifdef DEX_HAS_MONGO
    } else if (r->driver == DEX_DB_DRIVER_MONGO) {
        bson_iter_t iter;
        if (!dex_db_mongo_iter_to(r->mongo_doc, col, &iter)) return 0;
        if (BSON_ITER_HOLDS_INT32(&iter)) return bson_iter_int32(&iter);
        if (BSON_ITER_HOLDS_INT64(&iter)) return (int)bson_iter_int64(&iter);
        if (BSON_ITER_HOLDS_DOUBLE(&iter)) return (int)bson_iter_double(&iter);
        if (BSON_ITER_HOLDS_UTF8(&iter)) return atoi(bson_iter_utf8(&iter, NULL));
        return 0;
#endif
    }
    return 0;
}

// ==========================================================================
// dex_db_col_str
// ==========================================================================
const char* dex_db_col_str(int rows, int col) {
    if (rows < 0 || rows >= DEX_DB_MAX_RESULTS) {
        char* empty = (char*)malloc(1);
        if (!empty) return strdup("");
        empty[0] = '\0';
        return empty;
    }
    DexDbResult* r = &dex_db_results[rows];

    if (r->driver == DEX_DB_DRIVER_SQLITE) {
        const unsigned char* text = sqlite3_column_text(r->sqlite_stmt, col);
        if (!text) {
            char* empty = (char*)malloc(1);
            if (!empty) return strdup("");
            empty[0] = '\0';
            return empty;
        }
        size_t len = strlen((const char*)text);
        char* copy = (char*)malloc(len + 1);
        if (!copy) return strdup("");
        memcpy(copy, text, len + 1);
        return copy;

#ifdef DEX_HAS_POSTGRES
    } else if (r->driver == DEX_DB_DRIVER_POSTGRES) {
        char* val = PQgetvalue(r->pg_result, r->pg_row, col);
        if (!val) {
            char* empty = (char*)malloc(1);
            if (!empty) return strdup("");
            empty[0] = '\0';
            return empty;
        }
        size_t len = (size_t)PQgetlength(r->pg_result, r->pg_row, col);
        char* copy = (char*)malloc(len + 1);
        if (!copy) return strdup("");
        memcpy(copy, val, len);
        copy[len] = '\0';
        return copy;
#endif

#ifdef DEX_HAS_MYSQL
    } else if (r->driver == DEX_DB_DRIVER_MYSQL) {
        if (!r->mysql_row || !r->mysql_row[col]) {
            char* empty = (char*)malloc(1);
            if (!empty) return strdup("");
            empty[0] = '\0';
            return empty;
        }
        size_t len = strlen(r->mysql_row[col]);
        char* copy = (char*)malloc(len + 1);
        if (!copy) return strdup("");
        memcpy(copy, r->mysql_row[col], len + 1);
        return copy;
#endif

#ifdef DEX_HAS_MONGO
    } else if (r->driver == DEX_DB_DRIVER_MONGO) {
        bson_iter_t iter;
        if (!dex_db_mongo_iter_to(r->mongo_doc, col, &iter)) {
            char* empty = (char*)malloc(1);
            if (!empty) return strdup("");
            empty[0] = '\0';
            return empty;
        }
        if (BSON_ITER_HOLDS_UTF8(&iter)) {
            const char* val = bson_iter_utf8(&iter, NULL);
            size_t len = strlen(val);
            char* copy = (char*)malloc(len + 1);
            if (!copy) return strdup("");
            memcpy(copy, val, len + 1);
            return copy;
        }
        // Convert non-string types to string
        char buf[64];
        if (BSON_ITER_HOLDS_INT32(&iter)) {
            snprintf(buf, sizeof(buf), "%d", bson_iter_int32(&iter));
        } else if (BSON_ITER_HOLDS_INT64(&iter)) {
            snprintf(buf, sizeof(buf), "%lld", (long long)bson_iter_int64(&iter));
        } else if (BSON_ITER_HOLDS_DOUBLE(&iter)) {
            snprintf(buf, sizeof(buf), "%g", bson_iter_double(&iter));
        } else if (BSON_ITER_HOLDS_BOOL(&iter)) {
            snprintf(buf, sizeof(buf), "%s", bson_iter_bool(&iter) ? "true" : "false");
        } else if (BSON_ITER_HOLDS_OID(&iter)) {
            bson_oid_to_string(bson_iter_oid(&iter), buf);
        } else {
            buf[0] = '\0';
        }
        size_t len = strlen(buf);
        char* copy = (char*)malloc(len + 1);
        if (!copy) return strdup("");
        memcpy(copy, buf, len + 1);
        return copy;
#endif
    }

    char* empty = (char*)malloc(1);
    if (!empty) return strdup("");
    empty[0] = '\0';
    return empty;
}

// ==========================================================================
// dex_db_col_dexstr — return DexString* directly (single alloc, single copy)
// ==========================================================================
DexString* dex_db_col_dexstr(int rows, int col) {
    if (rows < 0 || rows >= DEX_DB_MAX_RESULTS) {
        return dex_string_new("", 0);
    }
    DexDbResult* r = &dex_db_results[rows];

    if (r->driver == DEX_DB_DRIVER_SQLITE) {
        const unsigned char* text = sqlite3_column_text(r->sqlite_stmt, col);
        if (!text) return dex_string_new("", 0);
        int nbytes = sqlite3_column_bytes(r->sqlite_stmt, col);
        return dex_string_new((const char*)text, (size_t)nbytes);

#ifdef DEX_HAS_POSTGRES
    } else if (r->driver == DEX_DB_DRIVER_POSTGRES) {
        char* val = PQgetvalue(r->pg_result, r->pg_row, col);
        if (!val) return dex_string_new("", 0);
        int len = PQgetlength(r->pg_result, r->pg_row, col);
        return dex_string_new(val, (size_t)len);
#endif

#ifdef DEX_HAS_MYSQL
    } else if (r->driver == DEX_DB_DRIVER_MYSQL) {
        if (!r->mysql_row || !r->mysql_row[col]) return dex_string_new("", 0);
        size_t len = strlen(r->mysql_row[col]);
        return dex_string_new(r->mysql_row[col], len);
#endif

#ifdef DEX_HAS_MONGO
    } else if (r->driver == DEX_DB_DRIVER_MONGO) {
        bson_iter_t iter;
        if (!dex_db_mongo_iter_to(r->mongo_doc, col, &iter)) return dex_string_new("", 0);
        if (BSON_ITER_HOLDS_UTF8(&iter)) {
            uint32_t ulen = 0;
            const char* val = bson_iter_utf8(&iter, &ulen);
            return dex_string_new(val, (size_t)ulen);
        }
        // Convert non-string types to string
        char buf[64];
        if (BSON_ITER_HOLDS_INT32(&iter)) {
            snprintf(buf, sizeof(buf), "%d", bson_iter_int32(&iter));
        } else if (BSON_ITER_HOLDS_INT64(&iter)) {
            snprintf(buf, sizeof(buf), "%lld", (long long)bson_iter_int64(&iter));
        } else if (BSON_ITER_HOLDS_DOUBLE(&iter)) {
            snprintf(buf, sizeof(buf), "%g", bson_iter_double(&iter));
        } else if (BSON_ITER_HOLDS_BOOL(&iter)) {
            snprintf(buf, sizeof(buf), "%s", bson_iter_bool(&iter) ? "true" : "false");
        } else if (BSON_ITER_HOLDS_OID(&iter)) {
            bson_oid_to_string(bson_iter_oid(&iter), buf);
        } else {
            buf[0] = '\0';
        }
        return dex_string_new(buf, strlen(buf));
#endif
    }

    return dex_string_new("", 0);
}

// ==========================================================================
// dex_db_col_double
// ==========================================================================
double dex_db_col_double(int rows, int col) {
    if (rows < 0 || rows >= DEX_DB_MAX_RESULTS) return 0.0;
    DexDbResult* r = &dex_db_results[rows];

    if (r->driver == DEX_DB_DRIVER_SQLITE) {
        return sqlite3_column_double(r->sqlite_stmt, col);

#ifdef DEX_HAS_POSTGRES
    } else if (r->driver == DEX_DB_DRIVER_POSTGRES) {
        char* val = PQgetvalue(r->pg_result, r->pg_row, col);
        if (!val || val[0] == '\0') return 0.0;
        return atof(val);
#endif

#ifdef DEX_HAS_MYSQL
    } else if (r->driver == DEX_DB_DRIVER_MYSQL) {
        if (!r->mysql_row || !r->mysql_row[col]) return 0.0;
        return atof(r->mysql_row[col]);
#endif

#ifdef DEX_HAS_MONGO
    } else if (r->driver == DEX_DB_DRIVER_MONGO) {
        bson_iter_t iter;
        if (!dex_db_mongo_iter_to(r->mongo_doc, col, &iter)) return 0.0;
        if (BSON_ITER_HOLDS_DOUBLE(&iter)) return bson_iter_double(&iter);
        if (BSON_ITER_HOLDS_INT32(&iter)) return (double)bson_iter_int32(&iter);
        if (BSON_ITER_HOLDS_INT64(&iter)) return (double)bson_iter_int64(&iter);
        if (BSON_ITER_HOLDS_UTF8(&iter)) return atof(bson_iter_utf8(&iter, NULL));
        return 0.0;
#endif
    }
    return 0.0;
}

// ==========================================================================
// dex_db_col_bool
// ==========================================================================
_Bool dex_db_col_bool(int rows, int col) {
    if (rows < 0 || rows >= DEX_DB_MAX_RESULTS) return 0;
    DexDbResult* r = &dex_db_results[rows];

    if (r->driver == DEX_DB_DRIVER_SQLITE) {
        return sqlite3_column_int(r->sqlite_stmt, col) != 0;

#ifdef DEX_HAS_POSTGRES
    } else if (r->driver == DEX_DB_DRIVER_POSTGRES) {
        char* val = PQgetvalue(r->pg_result, r->pg_row, col);
        if (!val || val[0] == '\0') return 0;
        return (val[0] == 't' || val[0] == 'T' || val[0] == '1');
#endif

#ifdef DEX_HAS_MYSQL
    } else if (r->driver == DEX_DB_DRIVER_MYSQL) {
        if (!r->mysql_row || !r->mysql_row[col]) return 0;
        return (r->mysql_row[col][0] == '1' || r->mysql_row[col][0] == 't' || r->mysql_row[col][0] == 'T');
#endif

#ifdef DEX_HAS_MONGO
    } else if (r->driver == DEX_DB_DRIVER_MONGO) {
        bson_iter_t iter;
        if (!dex_db_mongo_iter_to(r->mongo_doc, col, &iter)) return 0;
        if (BSON_ITER_HOLDS_BOOL(&iter)) return bson_iter_bool(&iter);
        if (BSON_ITER_HOLDS_INT32(&iter)) return bson_iter_int32(&iter) != 0;
        if (BSON_ITER_HOLDS_UTF8(&iter)) {
            const char* v = bson_iter_utf8(&iter, NULL);
            return (v[0] == 't' || v[0] == 'T' || v[0] == '1');
        }
        return 0;
#endif
    }
    return 0;
}

// ==========================================================================
// dex_db_free
// ==========================================================================
void dex_db_free(int rows) {
    if (rows < 0 || rows >= DEX_DB_MAX_RESULTS) return;
    DexDbResult* r = &dex_db_results[rows];

    if (r->driver == DEX_DB_DRIVER_SQLITE) {
        if (r->sqlite_stmt) sqlite3_finalize(r->sqlite_stmt);
    }
#ifdef DEX_HAS_POSTGRES
    else if (r->driver == DEX_DB_DRIVER_POSTGRES) {
        // The connection went back at query time; only the rows are left.
        if (r->pg_result) PQclear(r->pg_result);
    }
#endif
#ifdef DEX_HAS_MYSQL
    else if (r->driver == DEX_DB_DRIVER_MYSQL) {
        if (r->mysql_result) mysql_free_result(r->mysql_result);
    }
#endif
#ifdef DEX_HAS_MONGO
    else if (r->driver == DEX_DB_DRIVER_MONGO) {
        if (r->mongo_cursor) mongoc_cursor_destroy(r->mongo_cursor);
    }
#endif
    pthread_mutex_lock(&dex_db_mutex);
    memset(r, 0, sizeof(DexDbResult));
    pthread_mutex_unlock(&dex_db_mutex);
}

// ==========================================================================
// dex_db_close
// ==========================================================================
void dex_db_close(int conn) {
    if (conn < 0 || conn >= DEX_DB_MAX_CONNS) return;
    DexDbConn* c = &dex_db_conns[conn];

    if (c->driver == DEX_DB_DRIVER_SQLITE) {
        if (c->sqlite_conn) sqlite3_close(c->sqlite_conn);
    }
#ifdef DEX_HAS_POSTGRES
    else if (c->driver == DEX_DB_DRIVER_POSTGRES) {
        DexDbPool* pool = &c->pg_pool;
        for (int i = 0; i < pool->size; i++) {
            if (pool->connections[i]) PQfinish(pool->connections[i]);
        }
        pthread_mutex_destroy(&pool->lock);
        pthread_cond_destroy(&pool->available);
    }
#endif
#ifdef DEX_HAS_MYSQL
    else if (c->driver == DEX_DB_DRIVER_MYSQL) {
        if (c->mysql_conn) mysql_close(c->mysql_conn);
    }
#endif
#ifdef DEX_HAS_MONGO
    else if (c->driver == DEX_DB_DRIVER_MONGO) {
        if (c->mongo_client) mongoc_client_destroy(c->mongo_client);
    }
#endif
    pthread_mutex_lock(&dex_db_mutex);
    memset(c, 0, sizeof(DexDbConn));
    pthread_mutex_unlock(&dex_db_mutex);
}

// ==========================================================================
// dex_db_prepare — compile a SQL statement with parameter placeholders
// ==========================================================================
int dex_db_prepare(int conn, const char* sql) {
    if (conn < 0 || conn >= DEX_DB_MAX_CONNS) return -1;
    DexDbConn* c = &dex_db_conns[conn];

    pthread_mutex_lock(&dex_db_mutex);
    int slot = -1;
    for (int i = 0; i < DEX_DB_MAX_STMTS; i++) {
        if (dex_db_stmts[i].driver == DEX_DB_DRIVER_NONE) {
            slot = i;
            dex_db_stmts[i].driver = -1; // reserve
            break;
        }
    }
    pthread_mutex_unlock(&dex_db_mutex);
    if (slot < 0) return -1;

    if (c->driver == DEX_DB_DRIVER_SQLITE) {
        sqlite3_stmt* stmt = NULL;
        int rc = sqlite3_prepare_v2(c->sqlite_conn, sql, -1, &stmt, NULL);
        if (rc != SQLITE_OK) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_stmts[slot], 0, sizeof(DexDbStmt));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        pthread_mutex_lock(&dex_db_mutex);
        dex_db_stmts[slot].driver = DEX_DB_DRIVER_SQLITE;
        dex_db_stmts[slot].conn_slot = conn;
        dex_db_stmts[slot].sqlite_stmt = stmt;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;

#ifdef DEX_HAS_POSTGRES
    } else if (c->driver == DEX_DB_DRIVER_POSTGRES) {
        // The statement is not tied to one connection: it is prepared on
        // whichever pool entry ends up running it, and again after that entry
        // reconnects. Preparing once here is what turns a bad statement into an
        // error from prepare() rather than from the first step().
        DexDbPool* pool = &c->pg_pool;
        int entry = dex_db_pool_acquire(pool);
        if (entry < 0) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_stmts[slot], 0, sizeof(DexDbStmt));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }

        pthread_mutex_lock(&dex_db_mutex);
        unsigned int seq = ++dex_db_stmt_seq;
        pthread_mutex_unlock(&dex_db_mutex);
        snprintf(dex_db_stmts[slot].pg_stmt_name, sizeof(dex_db_stmts[slot].pg_stmt_name),
                 "dex_ps_%u", seq);

        PGresult* res = PQprepare(pool->connections[entry], dex_db_stmts[slot].pg_stmt_name, sql, 0, NULL);
        if (PQresultStatus(res) != PGRES_COMMAND_OK) {
            fprintf(stderr, "db.prepare error: %s", PQerrorMessage(pool->connections[entry]));
            PQclear(res);
            dex_db_pool_release(pool, entry);
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_stmts[slot], 0, sizeof(DexDbStmt));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        PQclear(res);
        unsigned int prepared_at = pool->generation[entry];
        dex_db_pool_release(pool, entry);

        pthread_mutex_lock(&dex_db_mutex);
        dex_db_stmts[slot].driver = DEX_DB_DRIVER_POSTGRES;
        dex_db_stmts[slot].conn_slot = conn;
        memset(dex_db_stmts[slot].prepared_gen, 0, sizeof(dex_db_stmts[slot].prepared_gen));
        dex_db_stmts[slot].prepared_gen[entry] = prepared_at;
        dex_db_stmts[slot].pg_sql = strdup(sql);
        dex_db_stmts[slot].pg_param_count = 0;
        dex_db_stmts[slot].pg_param_cap = 8;
        dex_db_stmts[slot].pg_param_values = (char**)calloc(8, sizeof(char*));
        dex_db_stmts[slot].pg_param_lengths = (int*)calloc(8, sizeof(int));
        dex_db_stmts[slot].pg_result = NULL;
        dex_db_stmts[slot].pg_current_row = 0;
        dex_db_stmts[slot].pg_num_rows = 0;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;
#endif

#ifdef DEX_HAS_MYSQL
    } else if (c->driver == DEX_DB_DRIVER_MYSQL) {
        MYSQL_STMT* stmt = mysql_stmt_init(c->mysql_conn);
        if (!stmt) {
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_stmts[slot], 0, sizeof(DexDbStmt));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        if (mysql_stmt_prepare(stmt, sql, strlen(sql)) != 0) {
            mysql_stmt_close(stmt);
            pthread_mutex_lock(&dex_db_mutex);
            memset(&dex_db_stmts[slot], 0, sizeof(DexDbStmt));
            pthread_mutex_unlock(&dex_db_mutex);
            return -1;
        }
        pthread_mutex_lock(&dex_db_mutex);
        dex_db_stmts[slot].driver = DEX_DB_DRIVER_MYSQL;
        dex_db_stmts[slot].conn_slot = conn;
        dex_db_stmts[slot].mysql_stmt = stmt;
        pthread_mutex_unlock(&dex_db_mutex);
        return slot;
#endif
    }

    // Unsupported driver
    pthread_mutex_lock(&dex_db_mutex);
    memset(&dex_db_stmts[slot], 0, sizeof(DexDbStmt));
    pthread_mutex_unlock(&dex_db_mutex);
    return -1;
}

// ==========================================================================
// dex_db_bind_int
// ==========================================================================
void dex_db_bind_int(int stmt_id, int idx, int val) {
    if (stmt_id < 0 || stmt_id >= DEX_DB_MAX_STMTS) return;
    DexDbStmt* s = &dex_db_stmts[stmt_id];

    if (s->driver == DEX_DB_DRIVER_SQLITE) {
        sqlite3_bind_int(s->sqlite_stmt, idx, val);
#ifdef DEX_HAS_POSTGRES
    } else if (s->driver == DEX_DB_DRIVER_POSTGRES) {
        int pidx = idx - 1;
        while (pidx >= s->pg_param_cap) {
            s->pg_param_cap *= 2;
            s->pg_param_values = (char**)realloc(s->pg_param_values, s->pg_param_cap * sizeof(char*));
            s->pg_param_lengths = (int*)realloc(s->pg_param_lengths, s->pg_param_cap * sizeof(int));
        }
        if (pidx >= s->pg_param_count) s->pg_param_count = pidx + 1;
        char buf[32];
        snprintf(buf, sizeof(buf), "%d", val);
        free(s->pg_param_values[pidx]);
        s->pg_param_values[pidx] = strdup(buf);
        s->pg_param_lengths[pidx] = (int)strlen(buf);
#endif
#ifdef DEX_HAS_MYSQL
    } else if (s->driver == DEX_DB_DRIVER_MYSQL) {
        // MySQL bind handled at step time via MYSQL_BIND array
#endif
    }
}

// ==========================================================================
// dex_db_bind_double
// ==========================================================================
void dex_db_bind_double(int stmt_id, int idx, double val) {
    if (stmt_id < 0 || stmt_id >= DEX_DB_MAX_STMTS) return;
    DexDbStmt* s = &dex_db_stmts[stmt_id];

    if (s->driver == DEX_DB_DRIVER_SQLITE) {
        sqlite3_bind_double(s->sqlite_stmt, idx, val);
#ifdef DEX_HAS_POSTGRES
    } else if (s->driver == DEX_DB_DRIVER_POSTGRES) {
        int pidx = idx - 1;
        while (pidx >= s->pg_param_cap) {
            s->pg_param_cap *= 2;
            s->pg_param_values = (char**)realloc(s->pg_param_values, s->pg_param_cap * sizeof(char*));
            s->pg_param_lengths = (int*)realloc(s->pg_param_lengths, s->pg_param_cap * sizeof(int));
        }
        if (pidx >= s->pg_param_count) s->pg_param_count = pidx + 1;
        char buf[64];
        snprintf(buf, sizeof(buf), "%g", val);
        free(s->pg_param_values[pidx]);
        s->pg_param_values[pidx] = strdup(buf);
        s->pg_param_lengths[pidx] = (int)strlen(buf);
#endif
    }
}

// ==========================================================================
// dex_db_bind_str
// ==========================================================================
void dex_db_bind_str(int stmt_id, int idx, const char* val) {
    if (stmt_id < 0 || stmt_id >= DEX_DB_MAX_STMTS) return;
    DexDbStmt* s = &dex_db_stmts[stmt_id];

    if (s->driver == DEX_DB_DRIVER_SQLITE) {
        sqlite3_bind_text(s->sqlite_stmt, idx, val, -1, SQLITE_TRANSIENT);
#ifdef DEX_HAS_POSTGRES
    } else if (s->driver == DEX_DB_DRIVER_POSTGRES) {
        int pidx = idx - 1;
        while (pidx >= s->pg_param_cap) {
            s->pg_param_cap *= 2;
            s->pg_param_values = (char**)realloc(s->pg_param_values, s->pg_param_cap * sizeof(char*));
            s->pg_param_lengths = (int*)realloc(s->pg_param_lengths, s->pg_param_cap * sizeof(int));
        }
        if (pidx >= s->pg_param_count) s->pg_param_count = pidx + 1;
        free(s->pg_param_values[pidx]);
        s->pg_param_values[pidx] = strdup(val);
        s->pg_param_lengths[pidx] = (int)strlen(val);
#endif
    }
}

// ==========================================================================
// dex_db_step — execute one step of a prepared statement
// Returns 1 if a row is available (SQLITE_ROW), 0 otherwise (SQLITE_DONE)
// ==========================================================================
_Bool dex_db_step(int stmt_id) {
    if (stmt_id < 0 || stmt_id >= DEX_DB_MAX_STMTS) return 0;
    DexDbStmt* s = &dex_db_stmts[stmt_id];

    if (s->driver == DEX_DB_DRIVER_SQLITE) {
        int rc = sqlite3_step(s->sqlite_stmt);
        return rc == SQLITE_ROW;
#ifdef DEX_HAS_POSTGRES
    } else if (s->driver == DEX_DB_DRIVER_POSTGRES) {
        // First call after bind: execute the prepared statement. The connection
        // is borrowed for the duration — libpq allows one thread per PGconn,
        // and this used to run on connections[0] no matter who else held it.
        if (!s->pg_result) {
            if (s->conn_slot < 0 || s->conn_slot >= DEX_DB_MAX_CONNS) return 0;
            DexDbPool* pool = &dex_db_conns[s->conn_slot].pg_pool;
            int entry = dex_db_pool_acquire(pool);
            if (entry < 0) return 0;

            for (int attempt = 0; attempt < 2; attempt++) {
                PGconn* pg = pool->connections[entry];

                // This connection has either never carried the statement or has
                // reconnected since it did, and a reconnect takes the server's
                // copy with it.
                if (s->prepared_gen[entry] != pool->generation[entry]) {
                    PGresult* pr = PQprepare(pg, s->pg_stmt_name, s->pg_sql, 0, NULL);
                    if (PQresultStatus(pr) != PGRES_COMMAND_OK) {
                        int broken = dex_db_pg_broken(pg);
                        if (!broken) {
                            fprintf(stderr, "db.step prepare error: %s", PQerrorMessage(pg));
                        }
                        PQclear(pr);
                        if (!broken || attempt == 1) break;
                        if (!dex_db_pg_revive(pool, entry)) break;
                        continue;
                    }
                    PQclear(pr);
                    s->prepared_gen[entry] = pool->generation[entry];
                }

                s->pg_result = PQexecPrepared(pg, s->pg_stmt_name,
                    s->pg_param_count, (const char* const*)s->pg_param_values,
                    s->pg_param_lengths, NULL, 0);
                ExecStatusType st = PQresultStatus(s->pg_result);
                if (st == PGRES_TUPLES_OK || st == PGRES_COMMAND_OK) break;

                int broken = dex_db_pg_broken(pg);
                if (!broken) {
                    fprintf(stderr, "db.step error: %s", PQerrorMessage(pg));
                }
                PQclear(s->pg_result);
                s->pg_result = NULL;
                if (!broken || attempt == 1) break;
                if (!dex_db_pg_revive(pool, entry)) break;
            }

            // As in query(), the rows outlive the connection they came from.
            dex_db_pool_release(pool, entry);

            if (!s->pg_result) return 0;
            if (PQresultStatus(s->pg_result) == PGRES_COMMAND_OK) {
                s->pg_num_rows = 0;
                s->pg_current_row = 0;
                return 0;
            }
            s->pg_num_rows = PQntuples(s->pg_result);
            s->pg_current_row = 0;
        }
        if (s->pg_current_row < s->pg_num_rows) {
            s->pg_current_row++;
            return 1;
        }
        return 0;
#endif
    }
    return 0;
}

// ==========================================================================
// dex_db_reset — reset a prepared statement for re-execution with new bindings
// ==========================================================================
void dex_db_reset(int stmt_id) {
    if (stmt_id < 0 || stmt_id >= DEX_DB_MAX_STMTS) return;
    DexDbStmt* s = &dex_db_stmts[stmt_id];

    if (s->driver == DEX_DB_DRIVER_SQLITE) {
        sqlite3_reset(s->sqlite_stmt);
        sqlite3_clear_bindings(s->sqlite_stmt);
#ifdef DEX_HAS_POSTGRES
    } else if (s->driver == DEX_DB_DRIVER_POSTGRES) {
        if (s->pg_result) {
            PQclear(s->pg_result);
            s->pg_result = NULL;
        }
        s->pg_current_row = 0;
        s->pg_num_rows = 0;
        for (int i = 0; i < s->pg_param_count; i++) {
            free(s->pg_param_values[i]);
            s->pg_param_values[i] = NULL;
        }
        s->pg_param_count = 0;
#endif
    }
}

// ==========================================================================
// dex_db_finalize — destroy a prepared statement and release the slot
// ==========================================================================
void dex_db_finalize(int stmt_id) {
    if (stmt_id < 0 || stmt_id >= DEX_DB_MAX_STMTS) return;
    DexDbStmt* s = &dex_db_stmts[stmt_id];

    if (s->driver == DEX_DB_DRIVER_SQLITE) {
        if (s->sqlite_stmt) sqlite3_finalize(s->sqlite_stmt);
    }
#ifdef DEX_HAS_POSTGRES
    else if (s->driver == DEX_DB_DRIVER_POSTGRES) {
        if (s->pg_result) PQclear(s->pg_result);
        for (int i = 0; i < s->pg_param_count; i++) free(s->pg_param_values[i]);
        free(s->pg_param_values);
        free(s->pg_param_lengths);
        // Deallocate on every connection that still carries it. Statement
        // names are never reused, so anything missed here is only wasted memory
        // on the server until that connection is recycled.
        if (s->conn_slot >= 0 && s->conn_slot < DEX_DB_MAX_CONNS) {
            DexDbPool* pool = &dex_db_conns[s->conn_slot].pg_pool;
            char dealloc[96];
            snprintf(dealloc, sizeof(dealloc), "DEALLOCATE %s", s->pg_stmt_name);
            for (int i = 0; i < pool->size; i++) {
                if (s->prepared_gen[i] == 0) continue;
                if (!dex_db_pool_acquire_entry(pool, i)) continue;
                // Still the same socket it was prepared on, and still up: a
                // reconnect already took the server's copy with it.
                if (s->prepared_gen[i] == pool->generation[i] &&
                    !dex_db_pg_broken(pool->connections[i])) {
                    PGresult* res = PQexec(pool->connections[i], dealloc);
                    if (res) PQclear(res);
                }
                s->prepared_gen[i] = 0;
                dex_db_pool_release(pool, i);
            }
        }
        if (s->pg_sql) free(s->pg_sql);
    }
#endif
#ifdef DEX_HAS_MYSQL
    else if (s->driver == DEX_DB_DRIVER_MYSQL) {
        if (s->mysql_stmt) mysql_stmt_close(s->mysql_stmt);
    }
#endif
    pthread_mutex_lock(&dex_db_mutex);
    memset(s, 0, sizeof(DexDbStmt));
    pthread_mutex_unlock(&dex_db_mutex);
}

// ==========================================================================
// Bound parameters
//
// dex_db_query_params / dex_db_exec_params send the statement and its values
// to Postgres separately, through PQexecParams. The server never parses the
// values as SQL, so a quote, a semicolon or a comment marker inside one is
// data and cannot become syntax. This is the difference between escaping —
// which is a correctness argument you have to keep winning — and binding,
// which removes the question.
//
// Placeholders are Postgres's own $1, $2, ... numbered from one.
//
// Values arrive as text and paramTypes is NULL, so the server infers each
// parameter's type from where it appears: `WHERE id = $1` against a bigint
// column parses $1 as a bigint. That is why a string[] is enough of a call
// shape despite the language having no heterogeneous array — the caller
// stringifies with str.fromLong and friends, and the server does the rest.
//
// SQL NULL is deliberately NOT expressible as a parameter. Whether a value is
// null is a structural decision the caller already knows — it changes the
// statement, not just its data — so the caller writes the NULL keyword into
// the SQL. An in-band sentinel string would collide with real data for no gain.
//
// Postgres only. SQLite and MySQL spell placeholders differently, and silently
// accepting a statement written for one dialect on another is how a query that
// looks bound stops being bound.
// ==========================================================================

#ifdef DEX_HAS_POSTGRES
// Borrows the argument array; the returned vector points into the DexStrings
// and must not outlive the call.
static const char** dex_db_param_vector(DexArrayString* args, int* count) {
    int n = args ? args->len : 0;
    *count = n;
    if (n == 0) return NULL;
    const char** values = (const char**)malloc(sizeof(char*) * n);
    if (!values) { *count = 0; return NULL; }
    for (int i = 0; i < n; i++) {
        DexString* v = args->data[i];
        values[i] = v ? v->data : "";
    }
    return values;
}
#endif

int dex_db_query_params(int conn, const char* sql, DexArrayString* args) {
    if (conn < 0 || conn >= DEX_DB_MAX_CONNS) return -1;
    DexDbConn* c = &dex_db_conns[conn];

#ifdef DEX_HAS_POSTGRES
    if (c->driver != DEX_DB_DRIVER_POSTGRES) {
        fprintf(stderr, "db.queryParams: bound parameters require the postgres driver\n");
        return -1;
    }

    pthread_mutex_lock(&dex_db_mutex);
    int slot = -1;
    for (int i = 0; i < DEX_DB_MAX_RESULTS; i++) {
        if (dex_db_results[i].driver == DEX_DB_DRIVER_NONE) {
            slot = i;
            dex_db_results[i].driver = -1; // reserve slot
            break;
        }
    }
    pthread_mutex_unlock(&dex_db_mutex);
    if (slot < 0) return -1;

    int nparams = 0;
    const char** values = dex_db_param_vector(args, &nparams);

    DexDbPool* pool = &c->pg_pool;
    int entry = dex_db_pool_acquire(pool);
    if (entry < 0) {
        free((void*)values);
        pthread_mutex_lock(&dex_db_mutex);
        memset(&dex_db_results[slot], 0, sizeof(DexDbResult));
        pthread_mutex_unlock(&dex_db_mutex);
        return -1;
    }

    // Same two-attempt rule as dex_db_query: a connection can die between
    // checkout and use, but a statement Postgres rejects is not retried.
    PGresult* res = NULL;
    for (int attempt = 0; attempt < 2; attempt++) {
        res = PQexecParams(pool->connections[entry], sql, nparams,
                           NULL, values, NULL, NULL, 0);
        if (PQresultStatus(res) == PGRES_TUPLES_OK) break;
        int broken = dex_db_pg_broken(pool->connections[entry]);
        if (!broken) {
            fprintf(stderr, "db.queryParams error: %s", PQerrorMessage(pool->connections[entry]));
        }
        PQclear(res);
        res = NULL;
        if (!broken || attempt == 1) break;
        if (!dex_db_pg_revive(pool, entry)) break;
    }

    // A PGresult owns its rows outright, so the connection goes back now.
    dex_db_pool_release(pool, entry);
    free((void*)values);

    if (!res) {
        pthread_mutex_lock(&dex_db_mutex);
        memset(&dex_db_results[slot], 0, sizeof(DexDbResult));
        pthread_mutex_unlock(&dex_db_mutex);
        return -1;
    }

    pthread_mutex_lock(&dex_db_mutex);
    dex_db_results[slot].driver = DEX_DB_DRIVER_POSTGRES;
    dex_db_results[slot].pg_result = res;
    dex_db_results[slot].pg_row = -1;
    dex_db_results[slot].pg_nrows = PQntuples(res);
    dex_db_results[slot].pool_entry = -1;
    dex_db_results[slot].pool_conn = -1;
    pthread_mutex_unlock(&dex_db_mutex);
    return slot;
#else
    (void)sql; (void)args; (void)c;
    fprintf(stderr, "db.queryParams: this build has no postgres support\n");
    return -1;
#endif
}

int dex_db_exec_params(int conn, const char* sql, DexArrayString* args) {
    if (conn < 0 || conn >= DEX_DB_MAX_CONNS) return -1;
    DexDbConn* c = &dex_db_conns[conn];

#ifdef DEX_HAS_POSTGRES
    if (c->driver != DEX_DB_DRIVER_POSTGRES) {
        fprintf(stderr, "db.execParams: bound parameters require the postgres driver\n");
        return -1;
    }

    int nparams = 0;
    const char** values = dex_db_param_vector(args, &nparams);

    DexDbPool* pool = &c->pg_pool;
    int entry = dex_db_pool_acquire(pool);
    if (entry < 0) { free((void*)values); return -1; }

    int n = -1;
    for (int attempt = 0; attempt < 2; attempt++) {
        PGresult* res = PQexecParams(pool->connections[entry], sql, nparams,
                                     NULL, values, NULL, NULL, 0);
        ExecStatusType status = PQresultStatus(res);
        if (status == PGRES_COMMAND_OK || status == PGRES_TUPLES_OK) {
            char* affected = PQcmdTuples(res);
            n = (affected && affected[0] != '\0') ? atoi(affected) : 0;
            PQclear(res);
            break;
        }
        int broken = dex_db_pg_broken(pool->connections[entry]);
        if (!broken) {
            fprintf(stderr, "db.execParams error: %s", PQerrorMessage(pool->connections[entry]));
        }
        PQclear(res);
        if (!broken || attempt == 1) break;
        if (!dex_db_pg_revive(pool, entry)) break;
    }

    dex_db_pool_release(pool, entry);
    free((void*)values);
    return n;
#else
    (void)sql; (void)args; (void)c;
    fprintf(stderr, "db.execParams: this build has no postgres support\n");
    return -1;
#endif
}
