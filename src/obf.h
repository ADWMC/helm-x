// obf.h — compile-time string obfuscation macro
#pragma once
#include <cstddef>
#include <string>

namespace helmx {

// Compile-time XOR string obfuscation.
// Usage: OBF("secret string") -> std::string at runtime.
template <size_t N>
struct ObfStr {
    char data[N];
    constexpr ObfStr(const char (&s)[N]) : data{} {
        for (size_t i = 0; i < N; ++i) {
            data[i] = s[i] ^ (char)(0x5A + i);
        }
    }
    std::string get() const {
        std::string out;
        out.reserve(N - 1);
        for (size_t i = 0; i < N - 1; ++i) {
            out.push_back(data[i] ^ (char)(0x5A + i));
        }
        return out;
    }
};

}  // namespace helmx

#define OBF(s) (::helmx::ObfStr<sizeof(s)>(s).get())
