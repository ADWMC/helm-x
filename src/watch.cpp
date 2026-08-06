// watch.cpp — self-healing daemon
#include "watch.h"

#include "config.h"

#include <chrono>
#include <cstdio>
#include <thread>

namespace helmx {

int watch(int interval_sec) {
    if (interval_sec < 5) interval_sec = 5;
    std::printf("[helm-x] watch started (interval %ds) — Ctrl+C to stop\n", interval_sec);

    std::string home = find_codex_home();
    if (home.empty()) {
        std::fprintf(stderr, "[helm-x] codex home not found\n");
        return 1;
    }

    while (true) {
        if (!verify_injection(home)) {
            std::printf("[helm-x] injection broken, restoring...\n");
            inject_config(home);
            deploy_agents(home);
            if (verify_injection(home)) {
                std::printf("[helm-x] restored\n");
            } else {
                std::fprintf(stderr, "[helm-x] restore FAILED\n");
            }
        }
        std::this_thread::sleep_for(std::chrono::seconds(interval_sec));
    }
    return 0;
}

}  // namespace helmx
