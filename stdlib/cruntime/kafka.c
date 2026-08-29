// Kafka client.
//
// Unlike Redis, the Kafka protocol is not something to hand-roll: metadata,
// partition leadership, consumer groups and rebalancing are the bulk of it, and
// getting them subtly wrong loses messages. So this wraps librdkafka, which is
// found at build time by pkg-config.
//
// When librdkafka is absent the module still compiles and every call fails
// loudly rather than the program failing to build. A Dex program that only
// mentions kafka on a path it never takes should still run on a machine that
// has never heard of Kafka.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>

#ifdef DEX_HAS_KAFKA
#include <librdkafka/rdkafka.h>
#endif

#define DEX_KAFKA_MAX_CLIENTS 16

static char* dex_kafka_own(const char* s, size_t len) {
    char* out = (char*)malloc(len + 1);
    if (!out) return NULL;
    if (len > 0) memcpy(out, s, len);
    out[len] = '\0';
    return out;
}

static char* dex_kafka_empty(void) { return dex_kafka_own("", 0); }

#ifdef DEX_HAS_KAFKA

typedef struct {
    int used;
    int is_consumer;
    rd_kafka_t* rk;
    rd_kafka_topic_partition_list_t* subscription;
    char last_topic[256];
    char last_key[256];
    pthread_mutex_t lock;
} DexKafkaClient;

static DexKafkaClient dex_kafka_clients[DEX_KAFKA_MAX_CLIENTS];
static pthread_mutex_t dex_kafka_table_lock = PTHREAD_MUTEX_INITIALIZER;

static DexKafkaClient* dex_kafka_at(int h) {
    if (h < 0 || h >= DEX_KAFKA_MAX_CLIENTS) return NULL;
    if (!dex_kafka_clients[h].used) return NULL;
    return &dex_kafka_clients[h];
}

static int dex_kafka_slot(void) {
    pthread_mutex_lock(&dex_kafka_table_lock);
    int h = -1;
    for (int i = 0; i < DEX_KAFKA_MAX_CLIENTS; i++) {
        if (!dex_kafka_clients[i].used) {
            memset(&dex_kafka_clients[i], 0, sizeof(DexKafkaClient));
            dex_kafka_clients[i].used = 1;
            pthread_mutex_init(&dex_kafka_clients[i].lock, NULL);
            h = i;
            break;
        }
    }
    pthread_mutex_unlock(&dex_kafka_table_lock);
    return h;
}

// Delivery reports are the only way to learn a produce failed, since produce
// itself only enqueues.
static void dex_kafka_on_delivery(rd_kafka_t* rk, const rd_kafka_message_t* msg, void* opaque) {
    (void)rk; (void)opaque;
    if (msg->err) {
        fprintf(stderr, "[kafka] delivery failed: %s\n", rd_kafka_err2str(msg->err));
    }
}

int dex_kafka_producer(const char* brokers) {
    char errstr[512];
    rd_kafka_conf_t* conf = rd_kafka_conf_new();
    if (rd_kafka_conf_set(conf, "bootstrap.servers", brokers, errstr, sizeof(errstr)) != RD_KAFKA_CONF_OK) {
        fprintf(stderr, "[kafka] %s\n", errstr);
        rd_kafka_conf_destroy(conf);
        return -1;
    }
    rd_kafka_conf_set_dr_msg_cb(conf, dex_kafka_on_delivery);

    rd_kafka_t* rk = rd_kafka_new(RD_KAFKA_PRODUCER, conf, errstr, sizeof(errstr));
    if (!rk) {
        fprintf(stderr, "[kafka] %s\n", errstr);
        return -1;
    }

    int h = dex_kafka_slot();
    if (h < 0) { rd_kafka_destroy(rk); return -1; }
    dex_kafka_clients[h].rk = rk;
    dex_kafka_clients[h].is_consumer = 0;
    return h;
}

// Enqueues. The key decides the partition, so everything sharing a key keeps
// its order — which is why a charger tag makes a good key.
_Bool dex_kafka_produce(int h, const char* topic, const char* key, const char* value) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c || c->is_consumer) return 0;

    rd_kafka_resp_err_t err = rd_kafka_producev(
        c->rk,
        RD_KAFKA_V_TOPIC(topic),
        RD_KAFKA_V_KEY(key, strlen(key)),
        RD_KAFKA_V_VALUE((void*)value, strlen(value)),
        RD_KAFKA_V_MSGFLAGS(RD_KAFKA_MSG_F_COPY),
        RD_KAFKA_V_END);

    if (err) {
        fprintf(stderr, "[kafka] produce to %s failed: %s\n", topic, rd_kafka_err2str(err));
        return 0;
    }
    // Serves the delivery-report queue without blocking, so failures surface
    // near the produce that caused them.
    rd_kafka_poll(c->rk, 0);
    return 1;
}

// librdkafka connects lazily, so a handle exists whether or not a broker does.
// Asking for metadata is how you learn the difference — worth a health check at
// startup rather than discovering it in a delivery report later.
_Bool dex_kafka_ready(int h, int timeout_ms) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c) return 0;
    const struct rd_kafka_metadata* md = NULL;
    rd_kafka_resp_err_t err = rd_kafka_metadata(c->rk, 0, NULL, &md, timeout_ms);
    if (err) return 0;
    rd_kafka_metadata_destroy(md);
    return 1;
}

int dex_kafka_flush(int h, int timeout_ms) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c) return -1;
    rd_kafka_flush(c->rk, timeout_ms);
    return rd_kafka_outq_len(c->rk);
}

int dex_kafka_consumer(const char* brokers, const char* group_id) {
    char errstr[512];
    rd_kafka_conf_t* conf = rd_kafka_conf_new();
    if (rd_kafka_conf_set(conf, "bootstrap.servers", brokers, errstr, sizeof(errstr)) != RD_KAFKA_CONF_OK ||
        rd_kafka_conf_set(conf, "group.id", group_id, errstr, sizeof(errstr)) != RD_KAFKA_CONF_OK ||
        rd_kafka_conf_set(conf, "auto.offset.reset", "earliest", errstr, sizeof(errstr)) != RD_KAFKA_CONF_OK ||
        rd_kafka_conf_set(conf, "enable.auto.commit", "false", errstr, sizeof(errstr)) != RD_KAFKA_CONF_OK) {
        fprintf(stderr, "[kafka] %s\n", errstr);
        rd_kafka_conf_destroy(conf);
        return -1;
    }

    rd_kafka_t* rk = rd_kafka_new(RD_KAFKA_CONSUMER, conf, errstr, sizeof(errstr));
    if (!rk) {
        fprintf(stderr, "[kafka] %s\n", errstr);
        return -1;
    }
    rd_kafka_poll_set_consumer(rk);

    int h = dex_kafka_slot();
    if (h < 0) { rd_kafka_destroy(rk); return -1; }
    dex_kafka_clients[h].rk = rk;
    dex_kafka_clients[h].is_consumer = 1;
    dex_kafka_clients[h].subscription = rd_kafka_topic_partition_list_new(4);
    return h;
}

_Bool dex_kafka_subscribe(int h, const char* topic) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c || !c->is_consumer) return 0;

    pthread_mutex_lock(&c->lock);
    rd_kafka_topic_partition_list_add(c->subscription, topic, RD_KAFKA_PARTITION_UA);
    rd_kafka_resp_err_t err = rd_kafka_subscribe(c->rk, c->subscription);
    pthread_mutex_unlock(&c->lock);

    if (err) {
        fprintf(stderr, "[kafka] subscribe to %s failed: %s\n", topic, rd_kafka_err2str(err));
        return 0;
    }
    return 1;
}

// Waits up to timeout_ms for one message and returns its value. An empty string
// means nothing arrived — which is ordinary, not an error.
char* dex_kafka_poll(int h, int timeout_ms) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c || !c->is_consumer) return dex_kafka_empty();

    rd_kafka_message_t* msg = rd_kafka_consumer_poll(c->rk, timeout_ms);
    if (!msg) return dex_kafka_empty();

    if (msg->err) {
        if (msg->err != RD_KAFKA_RESP_ERR__PARTITION_EOF) {
            fprintf(stderr, "[kafka] %s\n", rd_kafka_message_errstr(msg));
        }
        rd_kafka_message_destroy(msg);
        return dex_kafka_empty();
    }

    pthread_mutex_lock(&c->lock);
    const char* topic = rd_kafka_topic_name(msg->rkt);
    snprintf(c->last_topic, sizeof(c->last_topic), "%s", topic ? topic : "");
    if (msg->key && msg->key_len > 0) {
        size_t n = msg->key_len < sizeof(c->last_key) - 1 ? msg->key_len : sizeof(c->last_key) - 1;
        memcpy(c->last_key, msg->key, n);
        c->last_key[n] = '\0';
    } else {
        c->last_key[0] = '\0';
    }
    pthread_mutex_unlock(&c->lock);

    char* out = msg->payload ? dex_kafka_own((const char*)msg->payload, msg->len) : dex_kafka_empty();
    rd_kafka_message_destroy(msg);
    return out;
}

char* dex_kafka_last_topic(int h) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c) return dex_kafka_empty();
    return dex_kafka_own(c->last_topic, strlen(c->last_topic));
}

char* dex_kafka_last_key(int h) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c) return dex_kafka_empty();
    return dex_kafka_own(c->last_key, strlen(c->last_key));
}

// Auto-commit is off: a consumer commits once the message is safely handled,
// so a crash replays it rather than losing it.
_Bool dex_kafka_commit(int h) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c || !c->is_consumer) return 0;
    rd_kafka_resp_err_t err = rd_kafka_commit(c->rk, NULL, 0);
    if (err) {
        fprintf(stderr, "[kafka] commit failed: %s\n", rd_kafka_err2str(err));
        return 0;
    }
    return 1;
}

void dex_kafka_close(int h) {
    DexKafkaClient* c = dex_kafka_at(h);
    if (!c) return;

    if (c->is_consumer) {
        rd_kafka_consumer_close(c->rk);
        if (c->subscription) rd_kafka_topic_partition_list_destroy(c->subscription);
    } else {
        rd_kafka_flush(c->rk, 5000);
    }
    rd_kafka_destroy(c->rk);

    pthread_mutex_lock(&dex_kafka_table_lock);
    c->used = 0;
    pthread_mutex_unlock(&dex_kafka_table_lock);
}

_Bool dex_kafka_available(void) { return 1; }

#else  // no librdkafka at build time

static void dex_kafka_unavailable(void) {
    static int warned = 0;
    if (!warned) {
        warned = 1;
        fprintf(stderr, "[kafka] built without librdkafka — install it and rebuild:\n");
        fprintf(stderr, "        macOS:  brew install librdkafka\n");
        fprintf(stderr, "        Linux:  apt install librdkafka-dev\n");
    }
}

int dex_kafka_producer(const char* brokers) { (void)brokers; dex_kafka_unavailable(); return -1; }
_Bool dex_kafka_produce(int h, const char* t, const char* k, const char* v) { (void)h; (void)t; (void)k; (void)v; dex_kafka_unavailable(); return 0; }
int dex_kafka_flush(int h, int ms) { (void)h; (void)ms; return -1; }
int dex_kafka_consumer(const char* b, const char* g) { (void)b; (void)g; dex_kafka_unavailable(); return -1; }
_Bool dex_kafka_subscribe(int h, const char* t) { (void)h; (void)t; dex_kafka_unavailable(); return 0; }
char* dex_kafka_poll(int h, int ms) { (void)h; (void)ms; return dex_kafka_empty(); }
char* dex_kafka_last_topic(int h) { (void)h; return dex_kafka_empty(); }
char* dex_kafka_last_key(int h) { (void)h; return dex_kafka_empty(); }
_Bool dex_kafka_ready(int h, int ms) { (void)h; (void)ms; return 0; }
_Bool dex_kafka_commit(int h) { (void)h; return 0; }
void dex_kafka_close(int h) { (void)h; }
_Bool dex_kafka_available(void) { return 0; }

#endif
