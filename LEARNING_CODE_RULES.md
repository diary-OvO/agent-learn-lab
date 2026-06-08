# Go Learning Code Rules

> **核心**：学习项目，不是生产框架。Go 复现 Python 原课，优先"可对照" > "工程化"。

---

## 1. 基本定位

- 学习项目，复现 Python 原课机制
- 每个 Sx 独立可运行、可对照
- 模块服务课程，不提前扩展

## 2. 项目结构

```text
sXX_topic/        # 每节课独立 agent loop
internal/         # 跨章节最小复用模块
  ├── loopinit/   # 各 Sx 的 tool/hook 初始化
  ├── tools/      # Tool 实现
  ├── hooks/      # Hook 框架
  └── ...
```

### 当前设计

每个 `sXX_topic/main.go`：

```go
func main() {
    // 基础初始化
    client := modelclient.NewFromEnv(...)
    checker := permission.NewChecker(...)
    
    // Tool/Hook 初始化（loopinit）
    hookBus := hooks.NewHookBus()
    loopinit.InitS06Hooks(hookBus, checker, workDir)
    toolbox := loopinit.InitS06Toolbox(subAgent)
    
    // 主 loop（可见）
    for {
        resp := client.Chat.Completions.New(ctx, messages, toolbox.Schema())
        // 工具调用处理...
    }
}
```

**关键**：主 loop 在 main.go 可见，工具列表在 `internal/loopinit/sXX.go`。

### loopinit 模块

按 Sx 分文件，展示工具演进：

```go
// internal/loopinit/s06.go
func InitS06Toolbox(subAgent *subagent.SubAgent) *v2.ToolBox {
    return v2.NewToolBox(
        tools.NewBashToolV2(),
        tools.NewReadFileToolV2(),
        // ...
        tools.NewTaskToolV2(subAgent),  // S06 新增
    )
}

func InitS06Hooks(hookBus *hooks.HookBus, checker *permission.Checker, workDir string) {
    hookimpl.RegisterS06DefaultHooks(hookBus, checker, workDir)
}
```

**工具演进**：
- S03: 5 个基础工具
- S05: +Glob, +TodoWrite (7个)
- S06: +Task (8个)
- S07: +LoadSkill (9个)

**为什么这样设计**：
- ✅ 消除重复（工具列表只定义一次）
- ✅ 保持可见（main.go 能看到用的是 S06）
- ✅ 递进清晰（s03.go → s08.go）
- ❌ 不要 `LoopConfig`、`NewLoop(level)`、插件系统

---

## 3. 防止过度设计

### 对齐原则

| Python | Go | 禁止 |
|--------|-----|-----|
| `def foo():` | `func Foo()` | ❌ `(m *Manager) Foo()` |
| `LIMIT = 50000` | `const Limit = 50000` | ❌ `cfg.LimitBytes` |
| `class Agent:` | `type Agent struct{}` | ✅ 允许 |

### 四个自查问题

1. **Python 原版是什么结构？** 函数 → Go 用函数；类 → Go 用结构体
2. **这个抽象在 Python 原版存在吗？** 如果没有 `Manager`、`Config`，Go 也不要加
3. **这个改动是为了什么？** "更工程化" → ❌；"Go 必须传参" → ✅
4. **对照时是否直观？** `m.SnipCompact()` vs `SnipCompact()` → 多了一层间接

### 典型案例

**函数 vs 方法**

```python
# Python
def snip_compact(messages, max_messages=50):
    if len(messages) <= max_messages: return messages
    return messages[:3] + [placeholder] + messages[-tail:]
```

❌ `func (m *Manager) SnipCompact(...)` （Python 没有 Manager）  
✅ `func SnipCompact(messages []Message, maxMessages int) []Message`

**常量 vs 配置对象**

```python
# Python
CONTEXT_LIMIT = 50000
KEEP_RECENT = 3
```

❌ `type Config struct { ContextLimitBytes int }` （Python 没有 Config）  
✅ `const (ContextLimit = 50000; KeepRecent = 3)`

**参数传递**

```python
# Python
def tool_budget(messages):
    persist_large_output(tid, content)  # 全局函数
```

❌ `m.PersistLargeOutput()` （通过对象隐藏依赖）  
✅ `PersistLargeOutput(tid, content, workDir)` （显式传参）

### 允许的 Go 化

✅ **显式依赖**：Python 全局变量 → Go 参数  
✅ **错误处理**：Python 不处理 → Go 返回 error  
✅ **类型声明**：Python 动态 → Go 静态

---

## 4. 对标与克制

每个章节必须回答：
- 对标哪个 Sx？
- 新增什么机制？
- Go 相对 Python 有什么必要差异？

新增模块前必须问：
- **Python 原课有这个抽象吗？**
- 它让课程更易懂了吗？
- 它会让前面章节过度工程化吗？

**答不清楚，就不要抽象。**

---

## 5. 保护教学

- Python 原课是概念源头
- 保持 Sx 递进顺序
- 解释"为什么"，不只给代码

**禁止**：
- 把 Go 写成 Python 替代品
- 用复杂抽象掩盖原课简单机制
