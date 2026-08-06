// watch.h — self-healing daemon
#pragma once
namespace helmx {

// watch [interval_seconds] — verify injection integrity periodically, restore if broken
int watch(int interval_sec);

}  // namespace helmx
