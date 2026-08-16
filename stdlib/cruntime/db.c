
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
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

#define DEX_DB_DRIVER_NONE 0
#define DEX_DB_DRIVER_SQLITE 1
#define DEX_DB_DRIVER_POSTGRES 2
#define DEX_DB_DRIVER_MYSQL 3
#define DEX_DB_DRIVER_MONGO 4

#ifdef DEX_HAS_MONGO
static int dex_mongo_initialized = 0;
#endif

typedef struct {
    int driver;
    sqlite3* sqlite_conn;
#ifdef DEX_HAS_POSTGRES
    PGconn* pg_conn;
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

static DexDbConn dex_db_conns[DEX_DB_MAX_CONNS];
static DexDbResult dex_db_results[DEX_DB_MAX_RESULTS];

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
    int slot = -1;
    for (int i = 0; i < DEX_DB_MAX_CONNS; i++) {
        if (dex_db_conns[i].driver == DEX_DB_DRIVER_NONE) {
            slot = i;
            break;
        }
    }
    if (slot < 0) return -1;

    if (strcmp(driver, "sqlite") == 0) {
        sqlite3* db = NULL;
        int rc = sqlite3_open(dsn, &db);
        if (rc != SQLITE_OK) {
            if (db) sqlite3_close(db);
            return -1;
        }
        dex_db_conns[slot].driver = DEX_DB_DRIVER_SQLITE;
        dex_db_conns[slot].sqlite_conn = db;
        return slot;

#ifdef DEX_HAS_POSTGRES
    } else if (strcmp(driver, "postgres") == 0) {
        PGconn* pg = PQconnectdb(dsn);
        if (PQstatus(pg) != CONNECTION_OK) {
            PQfinish(pg);
            return -1;
        }
        dex_db_conns[slot].driver = DEX_DB_DRIVER_POSTGRES;
        dex_db_conns[slot].pg_conn = pg;
        return slot;
#endif

#ifdef DEX_HAS_MYSQL
    } else if (strcmp(driver, "mysql") == 0) {
        MYSQL* my = mysql_init(NULL);
        if (!my) return -1;

        char host[256], user[256], password[256], dbname[256];
        unsigned int port = 3306;
        dex_db_parse_mysql_dsn(dsn, host, user, password, dbname, &port);

        const char* db_param = dbname[0] ? dbname : NULL;
        const char* pass_param = password[0] ? password : NULL;

        if (!mysql_real_connect(my, host, user, pass_param, db_param, port, NULL, 0)) {
            mysql_close(my);
            return -1;
        }
        dex_db_conns[slot].driver = DEX_DB_DRIVER_MYSQL;
        dex_db_conns[slot].mysql_conn = my;
        return slot;
#endif

#ifdef DEX_HAS_MONGO
    } else if (strcmp(driver, "mongo") == 0) {
        if (!dex_mongo_initialized) {
            mongoc_init();
            dex_mongo_initialized = 1;
        }
        mongoc_client_t* client = mongoc_client_new(dsn);
        if (!client) return -1;

        // Extract database name from URI
        mongoc_uri_t* uri = mongoc_uri_new(dsn);
        const char* dbname = uri ? mongoc_uri_get_database(uri) : NULL;

        dex_db_conns[slot].driver = DEX_DB_DRIVER_MONGO;
        dex_db_conns[slot].mongo_client = client;
        if (dbname && dbname[0]) {
            strncpy(dex_db_conns[slot].mongo_dbname, dbname, 255);
            dex_db_conns[slot].mongo_dbname[255] = '\0';
        } else {
            strcpy(dex_db_conns[slot].mongo_dbname, "test");
        }
        if (uri) mongoc_uri_destroy(uri);
        return slot;
#endif
    }
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
        PGresult* res = PQexec(c->pg_conn, sql);
        ExecStatusType status = PQresultStatus(res);
        if (status != PGRES_COMMAND_OK && status != PGRES_TUPLES_OK) {
            PQclear(res);
            return -1;
        }
        char* affected = PQcmdTuples(res);
        int n = 0;
        if (affected && affected[0] != '\0') {
            n = atoi(affected);
        }
        PQclear(res);
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

    int slot = -1;
    for (int i = 0; i < DEX_DB_MAX_RESULTS; i++) {
        if (dex_db_results[i].driver == DEX_DB_DRIVER_NONE) {
            slot = i;
            break;
        }
    }
    if (slot < 0) return -1;

    if (c->driver == DEX_DB_DRIVER_SQLITE) {
        sqlite3_stmt* stmt = NULL;
        int rc = sqlite3_prepare_v2(c->sqlite_conn, sql, -1, &stmt, NULL);
        if (rc != SQLITE_OK) return -1;
        dex_db_results[slot].driver = DEX_DB_DRIVER_SQLITE;
        dex_db_results[slot].sqlite_stmt = stmt;
        dex_db_results[slot].sqlite_done = 0;
        return slot;

#ifdef DEX_HAS_POSTGRES
    } else if (c->driver == DEX_DB_DRIVER_POSTGRES) {
        PGresult* res = PQexec(c->pg_conn, sql);
        if (PQresultStatus(res) != PGRES_TUPLES_OK) {
            PQclear(res);
            return -1;
        }
        dex_db_results[slot].driver = DEX_DB_DRIVER_POSTGRES;
        dex_db_results[slot].pg_result = res;
        dex_db_results[slot].pg_row = -1;
        dex_db_results[slot].pg_nrows = PQntuples(res);
        return slot;
#endif

#ifdef DEX_HAS_MYSQL
    } else if (c->driver == DEX_DB_DRIVER_MYSQL) {
        if (mysql_query(c->mysql_conn, sql) != 0) return -1;
        MYSQL_RES* res = mysql_store_result(c->mysql_conn);
        if (!res) return -1;
        dex_db_results[slot].driver = DEX_DB_DRIVER_MYSQL;
        dex_db_results[slot].mysql_result = res;
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

        dex_db_results[slot].driver = DEX_DB_DRIVER_MONGO;
        dex_db_results[slot].mongo_cursor = cursor;
        return slot;
#endif
    }
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
        size_t len = strlen(val);
        char* copy = (char*)malloc(len + 1);
        if (!copy) return strdup("");
        memcpy(copy, val, len + 1);
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
    memset(r, 0, sizeof(DexDbResult));
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
        if (c->pg_conn) PQfinish(c->pg_conn);
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
    memset(c, 0, sizeof(DexDbConn));
}
