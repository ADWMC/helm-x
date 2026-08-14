// log.cpp - file logging to Claude Code home / helmx.log
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
    std::string home = find_claude_home();
    if (!home.empty()) {
        return (fs::path(home) / "helmx.log").string();
    }
    return "helmx.log";
}

std::string cyber_log_path() {
    std::string home = find_claude_home();
    if (!home.empty()) {
        return (fs::path(home) / "helmx-cyber.log").string();
    }
    return "helmx-cyber.log";
}

static void log_write(const std::string& level, const std::string& msg) {
    auto now = std::chrono::system_clock::now();
    std::time_t t = std::chrono::system_clock::to_time_t(now);
    char ts[32];
    std::strftime(ts, sizeof(ts), "%Y-%m-%d %H:%M:%S", std::localtime(&t));

    std::string line = std::string("[") + ts + "] [" + level + "] " + msg;

    std::lock_guard<std::mutex> lock(g_log_mutex);
    // append to log file
    std::ofstream f(log_path(), std::ios::app | std::ios::binary);
    if (f) {
        f << line << "\n";
    }
    // echo to console (real-time CLI window log stream)
    std::printf("%s\n", line.c_str());
    std::fflush(stdout);
}

void log_info(const std::string& msg) {
    log_write("INFO", msg);
}

void log_error(const std::string& msg) {
    log_write("ERROR", msg);
}

void log_cyber(const CyberContext& ctx) {
    auto now = std::chrono::system_clock::now();
    std::time_t t = std::chrono::system_clock::to_time_t(now);
    char ts[32];
    std::strftime(ts, sizeof(ts), "%Y-%m-%d %H:%M:%S", std::localtime(&t));

    // Build structured log entry
    std::string entry = std::string("[") + ts + "] CYBER_EVENT\n"
        + "  result: " + ctx.result + "\n"
        + "  prompt_mode: " + ctx.prompt_mode + "\n"
        + "  upstream_status: " + std::to_string(ctx.upstream_status) + "\n"
        + "  rewrite_status: " + (ctx.rewrite_status == 0 ? "not_attempted" :
                                   ctx.rewrite_status == 1 ? "success" : "failed") + "\n"
        + "  original: " + ctx.original.substr(0, 500) + "\n";

    if (!ctx.rewritten.empty()) {
        entry += "  rewritten: " + ctx.rewritten.substr(0, 500) + "\n";
    }
    if (!ctx.refusal_text.empty()) {
        entry += "  refusal: " + ctx.refusal_text.substr(0, 300) + "\n";
    }
    if (!ctx.trigger_words.empty()) {
        entry += "  triggers: " + ctx.trigger_words + "\n";
    }

    std::lock_guard<std::mutex> lock(g_log_mutex);
    // Append to cyber log
    std::ofstream f(cyber_log_path(), std::ios::app | std::ios::binary);
    if (f) {
        f << entry << "---\n";
        f.flush();
    }
    // Echo to console (don't call log_info — we already hold the mutex)
    std::printf("[CYBER] result=%s status=%d\n", ctx.result.c_str(), ctx.upstream_status);
    std::fflush(stdout);
}

}  // namespace helmx
