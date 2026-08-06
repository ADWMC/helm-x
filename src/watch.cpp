// watch.cpp — self-healing daemon (controllable background service)
#include "watch.h"

#include "config.h"

#include <atomic>
#include <chrono>
#include <cstdio>
#include <thread>

namespace helmx {

namespace {
std::atomic<bool> g_watch_running{false};
std::atomic<int> g_watch_restores{0};
std::atomic<long long> g_last_restore_ts{0};
std::thread g_watch_thread;

// verify + restore one pass. Returns true if injection was intact.
bool watch_pass(const std::string& home) {
    if (verify_injection(home)) return true;
    std::printf("[helm-x] injection broken, restoring...\n");
    std::fflush(stdout);
    inject_config(home);
    deploy_agents(home);
    if (verify_injection(home)) {
        g_watch_restores++;
        g_last_restore_ts = (long long)std::chrono::duration_cast<std::chrono::seconds>(
                                std::chrono::system_clock::now().time_since_epoch())
                                .count();
        std::printf("[helm-x] restored\n");
        std::fflush(stdout);
    } else {
        std::fprintf(stderr, "[helm-x] restore FAILED\n");
    }
    return false;
}
}  // namespace

void watch_start(int interval_sec) {
    if (g_watch_running.load()) return;
    if (interval_sec < 5) interval_sec = 5;
    g_watch_running = true;
    g_watch_thread = std::thread([interval_sec] {
        std::string home = find_codex_home();
        if (home.empty()) {
            std::fprintf(stderr, "[helm-x] codex home not found\n");
            g_watch_running = false;
            return;
        }
        std::printf("[helm-x] watch started (interval %ds)\n", interval_sec);
        std::fflush(stdout);
        while (g_watch_running.load()) {
            watch_pass(home);
            for (int i = 0; i < interval_sec && g_watch_running.load(); ++i) {
                std::this_thread::sleep_for(std::chrono::seconds(1));
            }
        }
        std::printf("[helm-x] watch stopped\n");
        std::fflush(stdout);
    });
    g_watch_thread.detach();
}

void watch_stop() {
    g_watch_running = false;
}

bool watch_running() {
    return g_watch_running.load();
}

int watch_restores() {
    return g_watch_restores.load();
}

long long watch_last_restore_ts() {
    return g_last_restore_ts.load();
}

int watch(int interval_sec) {
    if (interval_sec < 5) interval_sec = 5;
    std::printf("[helm-x] watch started (interval %ds) — Ctrl+C to stop\n", interval_sec);

    std::string home = find_codex_home();
    if (home.empty()) {
        std::fprintf(stderr, "[helm-x] codex home not found\n");
        return 1;
    }

    while (true) {
        watch_pass(home);
        std::this_thread::sleep_for(std::chrono::seconds(interval_sec));
    }
    return 0;
}

}  // namespace helmx
