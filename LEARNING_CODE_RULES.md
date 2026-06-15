# Go Learning Code Rules

> **核心**：这是 Go 版 Agent Loop 学习项目，不是生产框架。  
> 目标是用 Go 复现课程机制，并兼容 OpenAI 请求格式；优先保持 **可对照、可运行、可解释**，而不是追求工程化完整性。

---

## 1. 项目定位

`agent-learn-lab` 是一个按课程逐步递进的 Agent Loop 学习项目。

每一节课程目录负责展示当前阶段的主流程；`internal` 只沉淀跨章节复用的最小实现；`skills`、`examples`、`openaiadapter` 等模块都服务学习目标，而不是提前构造通用 Agent 框架。

基本原则：

```text
课程机制优先
主流程可见
模块边界清楚
Go 习惯适度
OpenAI 适配克制
不提前框架化
```

不要把项目写成：

```text
通用 Agent Runtime
插件平台
配置驱动框架
生产级多模型编排系统
```

---

## 2. 目录职责

当前目录结构：

```text
agent-learn-lab/
  sxx_xx/             # 每节课的主 loop 实现
    main.go           # 当前课程可运行入口

  internal/           # 课程共用核心实现
    modelclient       # 模型客户端初始化
    openaiadapter     # 工具 schema 到 OpenAI tool schema 的适配
    toolkit           # 工具注册、schema、执行抽象
    tools             # 具体工具实现
    permission        # 工具权限检查
    hooks             # HookBus 事件总线
    hookimpl          # 默认 hook 实现
    loopinit          # 各课程工具箱与 hook 初始化
    subagent          # 子 Agent 执行逻辑
    skills            # Skill 扫描与注册
    compact           # 上下文压缩与裁剪
    agentconsole      # 命令行输出格式化

  skills/             # 可被 Agent 加载的 Skill
    */SKILL.md
    */scripts
    */references

  examples/           # OpenAI API 调用示例
    chat_completions
    responses

  go.mod / go.sum
  .env.example
  LEARNING_CODE_RULES.md
```

### `sxx_xx`

每个 `sxx_xx/main.go` 是一节课的可运行入口。

它应该展示当前课程的 Agent Loop 主流程，例如：

```text
初始化模型客户端
初始化工具箱
初始化权限和 hook
维护 messages
读取用户输入
调用模型
处理 tool calls
追加 tool results
进入下一轮
```

学习者打开 `main.go`，应该能直接看出这一节课新增了什么机制。

不要让 `main.go` 只剩：

```text
agent.Run()
runtime.Start()
loop.Execute()
```

主 loop 必须保持可见。

---

### `internal`

`internal` 只放跨章节共用的最小实现。

模块应该服务课程，而不是服务未来想象中的框架。

可以沉淀：

```text
工具注册
工具实现
权限检查
hook 机制
subagent 逻辑
skill 扫描
上下文压缩
OpenAI 格式适配
命令行输出格式化
```

不要因为“以后可能会用到”提前新增模块。

---

### `loopinit`

`loopinit` 只做课程初始化。

它的职责是：

```text
按课程组装工具
按课程注册 hook
展示课程能力的递进
减少 main.go 中重复的工具列表
```

它不负责：

```text
控制主 loop
决定模型调用流程
执行工具调用
维护 messages
封装课程所有逻辑
```

换句话说：

```text
loopinit 可以回答：这一课用了哪些工具和 hook
loopinit 不应该接管：Agent 如何运行
```

---

## 3. 主 loop 必须可见

每个课程的 `main.go` 应该保留主 loop 骨架。

可以使用伪代码理解：

```text
初始化 client / toolbox / hooks / permission

for {
    读取用户输入
    追加 user message

    构造本轮 request messages
    调用 OpenAI Chat Completions

    追加 assistant message

    如果有 tool calls:
        执行工具
        追加 tool result messages
        继续请求模型

    输出最终回答
}
```

允许把细节放进函数，例如：

```text
输入读取
输出格式化
工具执行
消息转换
权限检查
hook 触发
```

但不要把主流程整体隐藏起来。

推荐：

```text
main.go 负责流程
internal 负责机制
工具和 hook 通过 loopinit 组装
OpenAI 格式通过 openaiadapter 适配
```

不推荐：

```text
main.go 只剩 agent.Run()
所有课程差异都藏在配置里
工具、hook、compact、subagent 全由 runtime 自动装配
```

---

## 4. Go 化原则

这个项目不是把原课逐行翻译成 Go，也不是借机重写生产级 Go 框架。

Go 化只允许解决必要问题：

```text
Go 需要显式类型
Go 需要显式错误处理
Go 不适合大量隐式全局变量
OpenAI SDK 类型比 Python dict 更严格
工具 schema 需要静态结构表达
```

Go 化不应该变成：

```text
提前设计 Config
提前设计 Manager
提前设计 Runtime
提前设计插件系统
提前设计接口层
```

判断标准：

```text
这个改动是否让课程机制更清楚？
这个改动是否只是 Go 语言必要适配？
这个改动是否隐藏了原课流程？
这个抽象是否会让 main.go 更难对照课程？
```

如果只是“看起来更工程化”，不要加。

---

## 5. 函数、结构体、方法

默认使用函数。

如果原课是普通函数，Go 里也优先写成包级函数。

例如：

```text
compact.Snip(...)
permission.Check(...)
skills.Scan(...)
```

不要为了统一风格强行写成：

```text
manager.Snip(...)
service.Check(...)
runtime.Scan(...)
```

---

### 允许轻量结构体

允许使用轻量结构体承载稳定状态。

轻量结构体适合表达：

```text
一个目录
一个索引文件
一组工具
一个 hook bus
一个权限检查器
一个子 Agent 的必要上下文
一个 Skill registry
```

它应该回答：

```text
这个对象持有哪些稳定状态？
这些状态是否反复被同一组操作使用？
用结构体是否比到处传参更清楚？
```

可以有：

```text
Store{Dir, Index}
ToolBox{tools}
Checker{workDir, rules}
HookBus{handlers}
Registry{skills}
```

不应该有：

```text
Manager{client, model, config, store, strategy}
Runtime{everything}
Service{dependencies...}
LoopConfig{enableA, enableB, enableC}
```

轻量结构体是为了表达稳定状态，不是为了隐藏课程流程。

---

### 方法只操作自身状态

如果一个函数主要操作结构体内部字段，可以做成方法。

如果一个函数是在表达课程流程，尤其涉及模型调用、消息流转、工具循环，就不要塞进方法里。

通用判断：

```text
读写自身状态 → 可以是方法
组织课程流程 → 不要塞进方法
调用模型并改变消息流 → 尽量保持显式
```

---

## 6. 常量优先，不提前配置化

课程里的固定值优先使用常量。

例如：

```text
上下文限制
裁剪阈值
默认保留消息数
默认工具输出上限
```

这些值在学习阶段通常是课程机制的一部分，不是用户配置系统。

推荐：

```text
const Limit = ...
const KeepRecent = ...
```

不推荐：

```text
Config.Limit
Config.KeepRecent
RuntimeOptions.EnableCompact
```

只有当课程明确进入“配置系统”或“多策略对比”时，才引入配置对象。

---

## 7. 接口不要提前出现

学习阶段默认使用具体类型。

接口只在当前真的需要时再引入，例如：

```text
同一能力有多个实现
测试必须 mock
课程正在讲抽象边界
确实需要替换后端
```

不要为了“以后可能替换”提前写接口。

不推荐：

```text
type LLMClient interface {}
type ToolRuntime interface {}
type AgentEngine interface {}
```

如果当前只有一个实现，直接使用具体类型更清楚。

---

## 8. OpenAI 适配边界

项目使用 OpenAI 请求格式兼容，但 OpenAI 适配不应该改变课程机制。

`openaiadapter` 的职责是：

```text
把 toolkit 的工具 schema 转成 OpenAI tools
把工具调用结果转成 OpenAI tool message
处理 OpenAI SDK 所需的消息类型转换
隔离 OpenAI 格式细节
```

它不应该负责：

```text
决定 Agent Loop 怎么运行
决定哪些工具启用
决定是否 compact
决定是否调用 subagent
保存长期状态
```

OpenAI Chat Completions 的基本流程应保持可见：

```text
messages + tools → model
assistant message → append
tool calls → execute tools
tool results → append
再次请求 model
```

尤其要注意：

```text
真实历史写入 messages
临时上下文只进入本轮 request 副本
```

真实历史包括：

```text
用户输入
assistant 回复
tool call
tool result
```

临时上下文包括：

```text
压缩提示
技能提示
记忆提示
额外系统提醒
本轮检索内容
```

临时上下文可以影响本次请求，但不要随意写回 `messages`。

---

## 9. 模块新增规则

新增 `internal` 模块前，先回答：

```text
1. 这个模块对应课程里的哪个机制？
2. 没有它，main.go 是否会重复或混乱？
3. 它是承载状态，还是隐藏流程？
4. 它是否会让前面章节也被迫改造？
```

如果答不清楚，就先不要新增模块。

模块应该小而明确。

推荐的模块名：

```text
tools
toolkit
hooks
hookimpl
permission
skills
compact
subagent
loopinit
openaiadapter
agentconsole
```

不推荐的模块名：

```text
utils
common
helper
manager
service
runtime
framework
engine
core
```

如果一个模块只能叫 `utils`，通常说明边界还没想清楚。

---

## 10. 每章递进规则

每个 `sxx_xx` 只展示当前课程新增的机制。

每章需要能说清楚：

```text
这一章对标哪个课程阶段？
相比上一章新增了什么？
主 loop 发生了什么变化？
新增 internal 模块是否必要？
Go 相比原课有什么必要差异？
OpenAI 格式带来了什么适配？
```

不要在早期章节提前引入后面章节才需要的结构。

例如：

```text
还没讲 hook，就不要出现 hook runtime
还没讲 subagent，就不要预留 subagent manager
还没讲 compact，就不要提前设计 context service
还没讲 memory，就不要提前放 memory config
```

课程递进比最终架构更重要。

---

## 11. 注释与说明

这个项目是学习项目，所以代码不仅要能跑，还要解释“为什么”。

注释应该解释：

```text
为什么这里要追加 tool message
为什么 request messages 是副本
为什么这个工具需要权限检查
为什么 hook 放在这个时机触发
为什么 compact 不能直接删除全部历史
```

注释不应该只是重复代码：

```text
// call function
// create variable
// return result
```

基于本规则输出的 Go 代码，凡是新增或改造的核心函数、方法、类型，都应该带有“对标 + 一句话总结”注释。

注释格式：

```text
// Xxx 对标 Python xxx。
//
// 一句话说明它在当前课程机制中的作用。
```

这类注释不是普通代码说明，而是学习项目的“对照标记”。

它应该回答两个问题：

```text
1. 这段 Go 代码对标原课里的哪个函数、类、变量或机制？
2. 它在当前 Agent Loop 课程中承担什么职责？
```

> **代码负责实现机制，注释负责建立“Go 实现 ↔ 原课机制”的对照关系。**

学习代码应该让读者看完后理解机制，而不是只看到封装后的结果。

---

## 12. 禁止清单

不要引入这些东西，除非课程明确需要：

```text
LoopConfig
AgentRuntime
AgentEngine
Manager
Service
Plugin System
Middleware Chain
全局依赖注入容器
复杂接口层
多模型策略框架
后台任务系统
生产级持久化层
```

也不要把简单机制包装成复杂对象：

```text
函数能说明白的，不要 Manager
常量能说明白的，不要 Config
显式传参能说明白的，不要依赖注入
main.go 能看清楚的，不要藏进 Runtime
```

---

## 13. 最终判断标准

写代码前，用这几句话自查：

```text
main.go 还能看清主 loop 吗？
这一章新增的机制是否一眼可见？
这个 internal 模块是否只服务当前课程？
这个结构体是否只承载稳定状态？
这个方法是否只操作自身状态？
OpenAI 适配是否只停留在格式边界？
有没有为了“以后扩展”提前抽象？
```

最终原则：

```text
默认函数
必要时使用轻量结构体
少用接口
常量优先
显式传参
主 loop 可见
模块服务课程
OpenAI 只做适配
不要 Manager
不要 Runtime
不要提前框架化
```

一句话总结：

> **用 Go 写出课程机制，而不是用课程机制写一个 Go 框架。**