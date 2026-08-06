// sse.h — SSE stream parser
#pragma once
#include <string>
#include <vector>

namespace helmx {

// Parse a single SSE event block (returns the event data lines)
// Feed incrementally: returns complete events as they arrive.
struct SseParser {
    std::string buffer;

    // Append chunk, extract complete events into out_events.
    // Each event = raw data lines joined with '\n'.
    void feed(const char* data, size_t len, std::vector<std::string>& out_events);
};

}  // namespace helmx
