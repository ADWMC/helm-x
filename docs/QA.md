# WebUI QA 维护说明

WebUI 的 QA 内容由 [`assets/qa.json`](../assets/qa.json) 提供。客户端启动后会尝试从 GitHub 默认分支读取最新版，失败时依次使用浏览器缓存和 EXE 内置副本。

## 添加问题

在 `items` 数组末尾增加一项：

```json
{
  "question": "问题标题",
  "answer": "可直接执行的处理步骤"
}
```

同时更新顶层字段：

```json
{
  "version": 2,
  "updated_at": "2026-08-14"
}
```

要求：

- `question` 和 `answer` 必须是非空字符串。
- 答案应给出明确路径、命令或操作入口。
- 不在 JSON 中放入 API Key、令牌、用户日志或其他敏感信息。
- 保持 JSON 为 UTF-8，并在提交前运行测试。

## 发布方式

只更新在线 QA 时，修改 `assets/qa.json` 并推送默认分支即可，用户点击“检查更新”后生效，无需重新发布 EXE。

新版本 EXE 需要同步内置 QA 时执行：

```powershell
python tools\embed.py
cmake --build build
python -m unittest discover -s tests -p "test_*.py" -v
```

## 回退顺序

1. GitHub：`https://raw.githubusercontent.com/ADWMC/helm-x/master/assets/qa.json`
2. 浏览器 `localStorage` 缓存
3. EXE 内置 `qa.json`

远端数据会经过结构校验，页面使用纯文本节点显示问答内容。
