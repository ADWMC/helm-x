// log.cpp — file logging to codex home / helmx.log (thread-safe, append)
#include "log.h"

#include "config.h"

#include <chrono>
#include <cstdio>
#include <fstream>
#include <mutex>
#include <filesystem>

namespace fs = std::filesystem;

namespace helmx {

namespace {
std::mutex g_log_mutex;
}

std::string log_path() {
    std::string home = find_codex_home();
    if (!home.empty()) {
        return (fs::path(home) / "helmx.log").string();
    }
    return "helmx.log";
}

static void log_write(const std::string& level, const std::string& msg) {
    auto now = std::chrono::system_clock::now();
    std::time_t t = std::chrono::system_clock::to_time_t(now);
    char ts[32];
    std::strftime(ts, sizeof(ts), "%Y-%m-%d %H:%M:%S", std::localtime(&t));

    std::lock_guard<std::mutex> lock(g_log_mutex);
    std::ofstream f(log_path(), std::ios::app | std::ios::binary);
    if (f) {
        f << "[" << ts << "] [" << level << "] " << msg << "\n";
    }
}

void log_info(const std::string& msg) {
    log_write("INFO", msg);
}

void log_error(const std::string& msg) {
    log_write("ERROR", msg);
}

}  // namespace helmx
