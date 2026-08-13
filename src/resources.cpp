// resources.cpp — embedded encrypted resource layer
// Cipher tables live in resources_generated.cpp (emitted by tools/embed.py).
#include "resources.h"

#include <cstdio>
#include <cstring>

namespace helmx {

// Declared in resources_generated.cpp
extern const unsigned char kAgentsMdCipher[];
extern const size_t kAgentsMdCipherLen;
extern const unsigned char kAgentsMdKey[];
extern const size_t kAgentsMdKeyLen;

extern const unsigned char kAgentsV45Cipher[];
extern const size_t kAgentsV45CipherLen;
extern const unsigned char kAgentsV45Key[];
extern const size_t kAgentsV45KeyLen;

extern const unsigned char kTamperRulesCipher[];
extern const size_t kTamperRulesCipherLen;
extern const unsigned char kTamperRulesKey[];
extern const size_t kTamperRulesKeyLen;

extern const unsigned char kDashboardHtmlCipher[];
extern const size_t kDashboardHtmlCipherLen;
extern const unsigned char kDashboardHtmlKey[];
extern const size_t kDashboardHtmlKeyLen;

extern const unsigned char kQaJsonCipher[];
extern const size_t kQaJsonCipherLen;
extern const unsigned char kQaJsonKey[];
extern const size_t kQaJsonKeyLen;

extern const unsigned char kRewritePromptCipher[];
extern const size_t kRewritePromptCipherLen;
extern const unsigned char kRewritePromptKey[];
extern const size_t kRewritePromptKeyLen;

extern const unsigned char kRewriterBuiltinCipher[];
extern const size_t kRewriterBuiltinCipherLen;
extern const unsigned char kRewriterBuiltinKey[];
extern const size_t kRewriterBuiltinKeyLen;

extern const char* const kSkillNames[];
extern const size_t kSkillCount;
struct SkillEntry {
    const char* name;
    const unsigned char* cipher;
    size_t cipher_len;
    const unsigned char* key;
    size_t key_len;
};
extern const SkillEntry kSkills[];

// XOR decrypt with per-resource key
static std::string xor_decrypt(const unsigned char* data, size_t len,
                               const unsigned char* key, size_t key_len) {
    if (!data || !key || len == 0 || key_len == 0) return {};
    std::string out;
    out.reserve(len);
    for (size_t i = 0; i < len; ++i) {
        out.push_back((char)(data[i] ^ key[i % key_len]));
    }
    return out;
}

std::string get_resource(ResId id) {
    switch (id) {
        case ResId::AgentsMd:
            return xor_decrypt(kAgentsMdCipher, kAgentsMdCipherLen, kAgentsMdKey, kAgentsMdKeyLen);
        case ResId::AgentsV45:
            return xor_decrypt(kAgentsV45Cipher, kAgentsV45CipherLen, kAgentsV45Key, kAgentsV45KeyLen);
        case ResId::TamperRules:
            return xor_decrypt(kTamperRulesCipher, kTamperRulesCipherLen, kTamperRulesKey, kTamperRulesKeyLen);
        case ResId::DashboardHtml:
            return xor_decrypt(kDashboardHtmlCipher, kDashboardHtmlCipherLen, kDashboardHtmlKey, kDashboardHtmlKeyLen);
        case ResId::QaJson:
            return xor_decrypt(kQaJsonCipher, kQaJsonCipherLen, kQaJsonKey, kQaJsonKeyLen);
        case ResId::RewritePrompt:
            return xor_decrypt(kRewritePromptCipher, kRewritePromptCipherLen, kRewritePromptKey, kRewritePromptKeyLen);
        case ResId::RewriterBuiltin:
            return xor_decrypt(kRewriterBuiltinCipher, kRewriterBuiltinCipherLen, kRewriterBuiltinKey, kRewriterBuiltinKeyLen);
        default:
            return "";
    }
}

std::string get_skill(const std::string& name) {
    for (size_t i = 0; i < kSkillCount; ++i) {
        if (kSkills[i].name && name == kSkills[i].name) {
            return xor_decrypt(kSkills[i].cipher, kSkills[i].cipher_len,
                               kSkills[i].key, kSkills[i].key_len);
        }
    }
    return "";
}

std::vector<std::string> list_skills() {
    std::vector<std::string> names;
    names.reserve(kSkillCount);
    for (size_t i = 0; i < kSkillCount; ++i) {
        if (kSkills[i].name) names.emplace_back(kSkills[i].name);
    }
    return names;
}

}  // namespace helmx
