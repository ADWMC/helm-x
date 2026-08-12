# helm-x 单元测试

参考 [gpt-5.6-instruct/unit-tests](https://github.com/MDX-Tom/gpt-5.6-instruct/tree/main/unit-tests) 的测试方法论，
针对 helm-x 的**部署逻辑、注入逻辑、TAMPER 引擎、改写器**编写自动化测试。

## 测试范围

| 模块 | 测试文件 | 覆盖内容 |
|---|---|---|
| config | `test_config.py` | apply/remove/restore、TOML 合并注入、CRLF 保留、备份回滚 |
| inject | `test_inject.py` | inject_request（Responses API input[] 格式、chat messages 格式）、AGENTS 注入后字节匹配 |
| tamper | `test_tamper.py` | TAMPER 规则加载、拒绝词检测、替换后文本完整性 |
| rewrite | `test_rewrite.py` | 改写器配置加载、本地规则匹配、API 改写调用（mock） |
| verify | `test_verify.py` | verify 检查项（config 注入、AGENTS 存在、备份存在、资源解密） |

## 运行

```bash
cd F:/projects/helm-x/tests
python -m pytest -v
```
