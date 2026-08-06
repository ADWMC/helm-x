// sse.cpp — SSE stream parser
#include "sse.h"

#include <vector>

namespace helmx {

void SseParser::feed(const char* data, size_t len, std::vector<std::string>& out_events) {
    buffer.append(data, len);

    size_t pos = 0;
    while (true) {
        // An SSE event terminates at a blank line: \n\n or \r\n\r\n
        size_t sep = buffer.find("\n\n", pos);
        size_t sep2 = buffer.find("\r\n\r\n", pos);
        size_t end;
        size_t sep_len;
        if (sep == std::string::npos) {
            if (sep2 == std::string::npos) break;  // incomplete
            end = sep2;
            sep_len = 4;
        } else if (sep2 == std::string::npos) {
            end = sep;
            sep_len = 2;
        } else {
            end = std::min(sep, sep2);
            sep_len = (end == sep) ? 2 : 4;
        }

        std::string block = buffer.substr(0, end);
        buffer.erase(0, end + sep_len);

        // extract data: lines
        std::string event_data;
        size_t lpos = 0;
        while (lpos < block.size()) {
            size_t le = block.find('\n', lpos);
            std::string line = block.substr(lpos, le == std::string::npos ? std::string::npos : le - lpos);
            if (line.size() > 5 && line.compare(0, 6, "data: ") == 0) {
                if (!event_data.empty()) event_data.push_back('\n');
                event_data.append(line.substr(6));
            }
            if (le == std::string::npos) break;
            lpos = le + 1;
        }
        if (!event_data.empty()) {
            out_events.push_back(event_data);
        }
    }
}

}  // namespace helmx
