
typedef const char* (*dex_handler_fn)(void);

typedef struct {
    const char* method;
    const char* path;
    dex_handler_fn handler;
} dex_route_entry;

#define DEX_MAX_ROUTES 64
static dex_route_entry dex_routes[DEX_MAX_ROUTES];
static int dex_route_count = 0;

void dex_route(const char* method, const char* path, dex_handler_fn handler) {
    if (dex_route_count < DEX_MAX_ROUTES) {
        dex_routes[dex_route_count].method = method;
        dex_routes[dex_route_count].path = path;
        dex_routes[dex_route_count].handler = handler;
        dex_route_count++;
    }
}

static void dex_send_response(int fd, const char* status, const char* body, int keep_alive) {
    int body_len = (int)strlen(body);
    char header[512];
    int hlen = snprintf(header, sizeof(header),
        "HTTP/1.1 %s\r\n"
        "Content-Type: application/json\r\n"
        "Content-Length: %d\r\n"
        "Connection: %s\r\n"
        "\r\n",
        status, body_len, keep_alive ? "keep-alive" : "close");
    // Use writev to send header+body in one syscall
    struct iovec iov[2];
    iov[0].iov_base = header;
    iov[0].iov_len = hlen;
    iov[1].iov_base = (void*)body;
    iov[1].iov_len = body_len;
    writev(fd, iov, 2);
}

static void dex_handle_connection(int client_fd) {
    char buf[4096];

    for (;;) {
        int n = (int)read(client_fd, buf, sizeof(buf) - 1);
        if (n <= 0) break;
        buf[n] = '\0';

        // Parse method and path from request line
        char method[16] = {0};
        char path[256] = {0};
        sscanf(buf, "%15s %255s", method, path);

        // Check for keep-alive (HTTP/1.1 defaults to keep-alive)
        int keep_alive = (strstr(buf, "HTTP/1.1") != NULL);
        if (strstr(buf, "Connection: close")) keep_alive = 0;

        // Match route
        const char* body = NULL;
        for (int i = 0; i < dex_route_count; i++) {
            if (strcmp(method, dex_routes[i].method) == 0 &&
                strcmp(path, dex_routes[i].path) == 0) {
                body = dex_routes[i].handler();
                break;
            }
        }

        if (body) {
            dex_send_response(client_fd, "200 OK", body, keep_alive);
        } else {
            dex_send_response(client_fd, "404 Not Found",
                "{\"error\": \"Not Found\"}", keep_alive);
        }

        if (!keep_alive) break;
    }
    close(client_fd);
}

static void* dex_worker(void* arg) {
    int client_fd = *(int*)arg;
    free(arg);
    dex_handle_connection(client_fd);
    return NULL;
}

static volatile int dex_server_fd = -1;

static void dex_shutdown_handler(int sig) {
    (void)sig;
    if (dex_server_fd >= 0) close(dex_server_fd);
    _exit(0);
}

void dex_listen(int port) {
    signal(SIGPIPE, SIG_IGN);
    signal(SIGINT, dex_shutdown_handler);
    signal(SIGTERM, dex_shutdown_handler);

    int server_fd = socket(AF_INET, SOCK_STREAM, 0);
    if (server_fd < 0) { perror("socket"); return; }
    dex_server_fd = server_fd;

    int opt = 1;
    setsockopt(server_fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

    struct sockaddr_in addr;
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = INADDR_ANY;
    addr.sin_port = htons(port);

    if (bind(server_fd, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        perror("bind"); close(server_fd); return;
    }

    if (listen(server_fd, 1024) < 0) {
        perror("listen"); close(server_fd); return;
    }

    printf("Dex server listening on port %d\n", port);
    fflush(stdout);

    while (1) {
        struct sockaddr_in client_addr;
        socklen_t client_len = sizeof(client_addr);
        int client_fd = accept(server_fd, (struct sockaddr*)&client_addr, &client_len);
        if (client_fd < 0) continue;

        int* fd_ptr = (int*)malloc(sizeof(int));
        *fd_ptr = client_fd;
        pthread_t tid;
        pthread_create(&tid, NULL, dex_worker, fd_ptr);
        pthread_detach(tid);
    }
}
