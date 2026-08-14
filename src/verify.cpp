// verify.cpp - built-in self-test for Claude Code integration
#include "verify.h"

#include "config.h"
#include "resources.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <sstream>
#include <string>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#pragma comment(lib, "advapi32.lib")
#endif

namespace fs = std::filesystem;

namespace helmx {

namespace {

int g_failures = 0;

void check(bool ok, const char* name, const char* detail, std::string& report) {
    char line[1024];
    std::snprintf(line, sizeof(line), "  [%s] %s %s\n", ok ? "PASS" : "FAIL", name, detail);
    report += line;
    if (!ok) g_failures++;
}

std::string read_file_str(const fs::path& p) {
    std::ifstream f(p, std::ios::binary);
    if (!f) return "";
    std::stringstream ss;
    ss << f.rdbuf();
    return ss.str();
}

// Run a command, capture stdout. Returns false if spawn failed.
bool run_capture(const std::string& cmd, std::string& out, int timeout_sec = 120) {
#ifdef _WIN32
    SECURITY_ATTRIBUTES sa{};
    sa.nLength = sizeof(sa);
    sa.bInheritHandle = TRUE;

    HANDLE read_pipe = nullptr, write_pipe = nullptr;
    if (!CreatePipe(&read_pipe, &write_pipe, &sa, 0)) return false;
    SetHandleInformation(read_pipe, HANDLE_FLAG_INHERIT, 0);

    STARTUPINFOA si{};
    si.cb = sizeof(si);
    si.hStdOutput = write_pipe;
    si.hStdError = write_pipe;
    si.dwFlags |= STARTF_USESTDHANDLES;

    PROCESS_INFORMATION pi{};
    std::string full_cmd = cmd;
    BOOL ok = CreateProcessA(
        nullptr, full_cmd.data(), nullptr, nullptr, TRUE,
        CREATE_NO_WINDOW, nullptr, nullptr, &si, &pi);
    CloseHandle(write_pipe);
    if (!ok) {
        CloseHandle(read_pipe);
        return false;
    }

    // read with timeout
    out.clear();
    char buf[4096];
    DWORD deadline = GetTickCount() + (DWORD)timeout_sec * 1000;
    for (;;) {
        DWORD avail = 0;
        if (PeekNamedPipe(read_pipe, nullptr, 0, nullptr, &avail, nullptr) && avail > 0) {
            DWORD n = 0;
            if (ReadFile(read_pipe, buf, sizeof(buf), &n, nullptr) && n > 0) {
                out.append(buf, n);
                continue;
            }
        }
        DWORD rc = WaitForSingleObject(pi.hProcess, 50);
        if (rc == WAIT_TIMEOUT) {
            if (GetTickCount() > deadline) {
                TerminateProcess(pi.hProcess, 1);
                break;
            }
            continue;
        }
        // drain remaining
        for (;;) {
            DWORD avail2 = 0;
            if (PeekNamedPipe(read_pipe, nullptr, 0, nullptr, &avail2, nullptr) && avail2 > 0) {
                DWORD n = 0;
                if (ReadFile(read_pipe, buf, sizeof(buf), &n, nullptr) && n > 0) {
                    out.append(buf, n);
                    continue;
                }
            }
            break;
        }
        break;
    }

    CloseHandle(pi.hProcess);
    CloseHandle(pi.hThread);
    CloseHandle(read_pipe);
    return true;
#else
    (void)cmd; (void)out; (void)timeout_sec;
    return false;
#endif
}

}  // namespace

// Claude Code is installed through an npm .cmd shim on Windows.
bool claude_exec_capture(std::string& out, int timeout_sec) {
    std::string cmd = "cmd /c \"claude -p helmx --output-format text 2>&1\"";
    return run_capture(cmd, out, timeout_sec);
}

int run_verify(bool e2e, std::string& report) {
    report.clear();
    g_failures = 0;

    report += "helm-x verify\n";
    report += "=============\n";

    // 1. Claude Code home
    std::string home = find_claude_home();
    check(!home.empty(), "Claude Code home", home.c_str(), report);
    if (home.empty()) {
        char line[128];
        std::snprintf(line, sizeof(line), "\n%d check(s) failed\n", g_failures);
        report += line;
        return 1;
    }

    // 2. settings.json exists
    fs::path cfg = fs::path(home) / "settings.json";
    check(fs::exists(cfg), "settings.json exists", cfg.string().c_str(), report);

    // 3. config injection state
    check(verify_injection(home), "ANTHROPIC_BASE_URL = helm-x", "", report);

    // 4. embedded resources are available to the proxy.
    int res_count = 0;
    if (!get_resource(ResId::PromptDefault).empty()) res_count++;
    if (!get_resource(ResId::TamperRules).empty()) res_count++;
    if (!get_resource(ResId::DashboardHtml).empty()) res_count++;
    if (!get_resource(ResId::RewritePrompt).empty()) res_count++;
    bool res_ok = res_count >= 3;
    std::string res_detail = std::to_string(res_count) + "/4";
    check(res_ok, "embedded resources decrypt", res_detail.c_str(), report);

    // 5. backup exists
    check(fs::exists(cfg.string() + ".helmx-bak"), "settings backup (.helmx-bak)", "", report);

    // 8. e2e
    if (e2e) {
        report += "  [....] e2e: claude -p \"helmx\"...\n";
        std::fflush(stdout);
        std::string out;
        bool spawned = claude_exec_capture(out, 240);
        // Check for both old and new activation responses
        bool activated = spawned && (
            out.find("Knowing you, I still like you") != std::string::npos ||
            out.find("helm-x online") != std::string::npos ||
            out.find("v45 online") != std::string::npos
        );
        if (!spawned) {
            std::string wout;
            run_capture("cmd /c \"where claude 2>&1\"", wout, 15);
            report += "  [....] where claude -> " + wout + "\n";
        }
        check(activated, "e2e: Claude activation (helmx)",
              activated ? "" : (spawned ? "(reply missing)" : "(Claude spawn failed)"), report);
    } else {
        report += "  [SKIP] e2e Claude check (run with --e2e)\n";
    }

    report += "=============\n";
    if (g_failures == 0) {
        report += e2e ? "ALL CHECKS PASSED (incl. e2e)\n" : "ALL CHECKS PASSED\n";
        return 0;
    }
    char line[128];
    std::snprintf(line, sizeof(line), "%d check(s) FAILED\n", g_failures);
    report += line;
    return 1;
}

int verify_main(int argc, char** argv) {
    bool e2e = false;
    for (int i = 1; i < argc; ++i) {
        if (std::strcmp(argv[i], "--e2e") == 0) e2e = true;
    }
    std::string report;
    int rc = run_verify(e2e, report);
    std::printf("%s", report.c_str());
    return rc;
}

int run_zxwn(std::string& out, bool& activated) {
    out.clear();
    activated = false;
    std::string raw;
    if (!claude_exec_capture(raw, 360)) {
        out = "[FAIL] could not run Claude Code (is it installed? run `where claude`)";
        return 1;
    }
    out = raw;
    // Check for both old and new activation responses
    activated = raw.find("Knowing you, I still like you") != std::string::npos ||
                raw.find("helm-x online") != std::string::npos ||
                raw.find("v45 online") != std::string::npos;
    return activated ? 0 : 1;
}

int zxwn_cmd() {
    std::printf("helm-x helmx - sending activation to Claude Code...\n");
    std::fflush(stdout);

    std::string out;
    bool activated = false;
    int rc = run_zxwn(out, activated);
    std::printf("%s\n", out.c_str());
    if (activated) {
        std::printf("[OK] activation confirmed\n");
        return 0;
    }
    std::fprintf(stderr, "[WARN] activation phrase not detected in Claude reply\n");
    return rc;
}

}  // namespace helmx
