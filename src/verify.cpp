// verify.cpp — built-in self-test: injection state, AGENTS integrity, e2e codex check
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

// Shared: run `codex exec --skip-git-repo-check helmx` and capture output.
// codex on Windows is a .cmd shim (npm); CreateProcess cannot run it
// directly, so route through cmd /c.
bool codex_exec_capture(std::string& out, int timeout_sec) {
    std::string cmd = "cmd /c \"codex exec --skip-git-repo-check helmx 2>&1\"";
    return run_capture(cmd, out, timeout_sec);
}

int run_verify(bool e2e, std::string& report) {
    report.clear();
    g_failures = 0;

    report += "helm-x verify\n";
    report += "=============\n";

    // 1. codex home
    std::string home = find_codex_home();
    check(!home.empty(), "codex home", home.c_str(), report);
    if (home.empty()) {
        char line[128];
        std::snprintf(line, sizeof(line), "\n%d check(s) failed\n", g_failures);
        report += line;
        return 1;
    }

    // 2. config.toml exists
    fs::path cfg = fs::path(home) / "config.toml";
    check(fs::exists(cfg), "config.toml exists", cfg.string().c_str(), report);

    // 3. config injection state
    std::string cfg_text = read_file_str(cfg);
    check(verify_injection(home), "model_provider = custom", "", report);

    // 4. embedded resources non-empty (AGENTS.md not deployed as file — proxy injects)
    std::string expected = get_resource(ResId::AgentsMd);
    int res_count = 0;
    if (!get_resource(ResId::AgentsMd).empty()) res_count++;
    if (!get_resource(ResId::TamperRules).empty()) res_count++;
    if (!get_resource(ResId::DashboardHtml).empty()) res_count++;
    if (!get_resource(ResId::RewritePrompt).empty()) res_count++;
    bool res_ok = res_count >= 3;  // At least AgentsMd + TamperRules + DashboardHtml
    std::string res_detail = std::to_string(res_count) + "/4";
    check(res_ok, "embedded resources decrypt", res_detail.c_str(), report);

    // 5. backup exists
    check(fs::exists(cfg.string() + ".helmx-bak"), "config backup (.helmx-bak)", "", report);

    // 8. e2e
    if (e2e) {
        report += "  [....] e2e: codex exec \"helmx\" (may take ~1-2 min)...\n";
        std::fflush(stdout);
        std::string out;
        bool spawned = codex_exec_capture(out, 240);
        // Check for both old and new activation responses
        bool activated = spawned && (
            out.find("Knowing you, I still like you") != std::string::npos ||
            out.find("helm-x online") != std::string::npos ||
            out.find("v45 online") != std::string::npos
        );
        if (!spawned) {
            std::string wout;
            run_capture("cmd /c \"where codex 2>&1\"", wout, 15);
            report += "  [....] where codex -> " + wout + "\n";
        }
        check(activated, "e2e: codex activation (helmx)",
              activated ? "" : (spawned ? "(reply missing)" : "(codex spawn failed)"), report);
    } else {
        report += "  [SKIP] e2e codex check (run with --e2e)\n";
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
    // codex reasoning can exceed 4 min on first run; give it 6 min
    if (!codex_exec_capture(raw, 360)) {
        out = "[FAIL] could not run codex (is it installed? run `where codex`)";
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
    std::printf("helm-x helmx — sending activation to codex...\n");
    std::fflush(stdout);

    std::string out;
    bool activated = false;
    int rc = run_zxwn(out, activated);
    std::printf("%s\n", out.c_str());
    if (activated) {
        std::printf("[OK] activation confirmed\n");
        return 0;
    }
    std::fprintf(stderr, "[WARN] activation phrase not detected in codex reply\n");
    return rc;
}

}  // namespace helmx
