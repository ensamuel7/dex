
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <dirent.h>
#include <curl/curl.h>

static int dex_file_name_cmp(const void* a, const void* b) {
    return strcmp(*(const char* const*)a, *(const char* const*)b);
}

// The entries of a directory, sorted, excluding "." and "..".
//
// Sorted because readdir order is whatever the filesystem feels like, and a
// caller walking migrations in name order cannot depend on that. An unreadable
// directory returns empty rather than failing: the caller can tell the
// difference from an empty one only if it cares, and most do not.
DexArrayString* dex_file_list(const char* path) {
    DexArrayString* out = dex_array_string_new();
    DIR* d = opendir(path);
    if (!d) return out;

    char** names = NULL;
    int count = 0, cap = 0;
    struct dirent* e;
    while ((e = readdir(d)) != NULL) {
        if (strcmp(e->d_name, ".") == 0 || strcmp(e->d_name, "..") == 0) continue;
        if (count == cap) {
            cap = cap ? cap * 2 : 16;
            char** grown = (char**)realloc(names, sizeof(char*) * cap);
            if (!grown) break;
            names = grown;
        }
        names[count] = strdup(e->d_name);
        if (!names[count]) break;
        count++;
    }
    closedir(d);

    if (names) {
        qsort(names, count, sizeof(char*), dex_file_name_cmp);
        for (int i = 0; i < count; i++) {
            DexString* s = dex_string_new(names[i], strlen(names[i]));
            dex_array_string_push(out, s);
            dex_release(s);
            free(names[i]);
        }
        free(names);
    }
    return out;
}

const char* dex_file_read(const char* path) {
    FILE* f = fopen(path, "rb");
    if (!f) {
        char* empty = (char*)malloc(1);
        empty[0] = '\0';
        return empty;
    }
    fseek(f, 0, SEEK_END);
    long len = ftell(f);
    fseek(f, 0, SEEK_SET);
    char* buf = (char*)malloc(len + 1);
    if (!buf) {
        fclose(f);
        char* empty = (char*)malloc(1);
        empty[0] = '\0';
        return empty;
    }
    fread(buf, 1, len, f);
    buf[len] = '\0';
    fclose(f);
    return buf;
}

_Bool dex_file_write(const char* path, const char* data) {
    FILE* f = fopen(path, "w");
    if (!f) return 0;
    size_t len = strlen(data);
    size_t written = fwrite(data, 1, len, f);
    fclose(f);
    return written == len;
}

_Bool dex_file_append(const char* path, const char* data) {
    FILE* f = fopen(path, "a");
    if (!f) return 0;
    size_t len = strlen(data);
    size_t written = fwrite(data, 1, len, f);
    fclose(f);
    return written == len;
}

_Bool dex_file_exists(const char* path) {
    return access(path, F_OK) == 0;
}

_Bool dex_file_remove(const char* path) {
    return remove(path) == 0;
}

_Bool dex_file_rename(const char* old_path, const char* new_path) {
    return rename(old_path, new_path) == 0;
}

struct dex_upload_buf {
    char* data;
    size_t size;
};

static size_t dex_upload_write_cb(void* contents, size_t size, size_t nmemb, void* userp) {
    size_t total = size * nmemb;
    struct dex_upload_buf* buf = (struct dex_upload_buf*)userp;
    char* ptr = (char*)realloc(buf->data, buf->size + total + 1);
    if (!ptr) return 0;
    buf->data = ptr;
    memcpy(buf->data + buf->size, contents, total);
    buf->size += total;
    buf->data[buf->size] = '\0';
    return total;
}

const char* dex_file_upload(const char* path, const char* url) {
    CURL* curl = curl_easy_init();
    if (!curl) {
        char* empty = (char*)malloc(1);
        empty[0] = '\0';
        return empty;
    }

    curl_mime* mime = curl_mime_init(curl);
    curl_mimepart* part = curl_mime_addpart(mime);
    curl_mime_name(part, "file");
    curl_mime_filedata(part, path);

    struct dex_upload_buf response = {NULL, 0};
    response.data = (char*)malloc(1);
    response.data[0] = '\0';

    curl_easy_setopt(curl, CURLOPT_URL, url);
    curl_easy_setopt(curl, CURLOPT_MIMEPOST, mime);
    curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, dex_upload_write_cb);
    curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response);

    CURLcode res = curl_easy_perform(curl);
    curl_mime_free(mime);
    curl_easy_cleanup(curl);

    if (res != CURLE_OK) {
        free(response.data);
        char* empty = (char*)malloc(1);
        empty[0] = '\0';
        return empty;
    }

    return response.data;
}
