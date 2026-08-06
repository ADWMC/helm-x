// watch.h — self-healing daemon (controllable background service)
#pragma once
#include <atomic>
#include <string>

namespace helmx {

// Start the self-healing loop in a background thread.
// interval_sec: check period. Returns immediately.
void watch_start(int interval_sec);

// Request the loop to stop (best-effort; current iteration finishes).
void watch_stop();

// True while the loop is running.
bool watch_running();

// Number of successful restores performed so far.
int watch_restores();

// Last restore timestamp (unix epoch seconds), 0 if never.
long long watch_last_restore_ts();

// Blocking loop used by the `watch` CLI command (foreground mode).
int watch(int interval_sec);

}  // namespace helmx
