// DexLang Concurrency Runtime
// Provides channels and task handles using pthreads.

#include <pthread.h>
#include <stdlib.h>
#include <string.h>

// === Channel (blocking bounded queue) ===
typedef struct {
    void* data;
    int elem_size;
    int cap;
    int len;
    int head;
    int tail;
    int closed;
    pthread_mutex_t lock;
    pthread_cond_t not_empty;
    pthread_cond_t not_full;
} DexChan;

DexChan* dex_chan_new(int elem_size, int cap) {
    DexChan* ch = (DexChan*)malloc(sizeof(DexChan));
    ch->elem_size = elem_size;
    ch->cap = cap;
    ch->len = 0;
    ch->head = 0;
    ch->tail = 0;
    ch->closed = 0;
    ch->data = malloc(elem_size * cap);
    pthread_mutex_init(&ch->lock, NULL);
    pthread_cond_init(&ch->not_empty, NULL);
    pthread_cond_init(&ch->not_full, NULL);
    return ch;
}

void dex_chan_send(DexChan* ch, void* val) {
    pthread_mutex_lock(&ch->lock);
    while (ch->len == ch->cap && !ch->closed) {
        pthread_cond_wait(&ch->not_full, &ch->lock);
    }
    if (ch->closed) {
        pthread_mutex_unlock(&ch->lock);
        return;
    }
    memcpy((char*)ch->data + ch->tail * ch->elem_size, val, ch->elem_size);
    ch->tail = (ch->tail + 1) % ch->cap;
    ch->len++;
    pthread_cond_signal(&ch->not_empty);
    pthread_mutex_unlock(&ch->lock);
}

void dex_chan_recv(DexChan* ch, void* out) {
    pthread_mutex_lock(&ch->lock);
    while (ch->len == 0 && !ch->closed) {
        pthread_cond_wait(&ch->not_empty, &ch->lock);
    }
    if (ch->len == 0 && ch->closed) {
        memset(out, 0, ch->elem_size);
        pthread_mutex_unlock(&ch->lock);
        return;
    }
    memcpy(out, (char*)ch->data + ch->head * ch->elem_size, ch->elem_size);
    ch->head = (ch->head + 1) % ch->cap;
    ch->len--;
    pthread_cond_signal(&ch->not_full);
    pthread_mutex_unlock(&ch->lock);
}

void dex_chan_close(DexChan* ch) {
    pthread_mutex_lock(&ch->lock);
    ch->closed = 1;
    pthread_cond_broadcast(&ch->not_empty);
    pthread_cond_broadcast(&ch->not_full);
    pthread_mutex_unlock(&ch->lock);
}
