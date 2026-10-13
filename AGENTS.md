# MajSpirit — agent 交接上下文

雀灵麻将（MajSpirit）：在线立直麻将服务端，来自一份实习任务书。
Go 1.27 · 单模块 · PostgreSQL · 无 Web 框架依赖。

**动手前读 [`docs/local/实现指南.md`](docs/local/实现指南.md)** —— 它是这套文档的主线：
进度表、环境注意事项、读者假设、测试判据、注释判据，以及分册索引。下面只写
**从代码、配置和目录结构里查不到的东西**。

---

## 怎么和他配合

项目的所有者是**联赛一等水平的算法选手**，写过高性能物理引擎、SDL2 游戏、
Godot 项目，六年都在造轮子。**第一次写 Go、第一次写服务端。**

- **先讲机制，再给代码。** 他会追问"为什么"，而且追问通常是对的 ——
  上一段会话里他每次反驳都逼出了更准的答案，其中两次查出了真 bug：
  一个把 401 变成假的 500，一个把 400 变成 **201**（带多余字段的注册请求被静默接受）。
  他问"你确定这是更好的设计吗"时是在要**判据**，不是在要安慰：给反例和代价。
- **他说"不要 X""一行都不想要了"，是删代码的信号**，不是优化信号。
- **过度设计是他的红线。** 加任何抽象之前先答一句：
  **它让我能测到原本测不了的东西吗？**
  先例：`user.Repo` 值（假实现让"并发注册""两条失败路径耗时接近"不必连库）；
  `Hasher` / `TokenIssuer` 不值（`auth` 的函数是纯的，加接口只会让测试离真代码更远）。
- **中文，短句。** 术语用 索 / 筒 / 东 / 白板 / 发财 / 红中。
- 他会横跨几层连问（一层是语言机制、下一层是设计）。答完把落点说清，
  别留他在"这两个回答怎么接起来"上。

**他会栽的地方按危险程度排在 `实现指南.md` 的「读者假设」一节** —— 并发、不可信对端、
错误处理（他自评"从来不写"）、SQL、框架惯例。读那一节，别在这里重复它。

## 他的目标

按任务书走六个阶段：规则层 → 用户系统 → WebSocket → 房间与对局 → 历史牌局 → 加分项。
**进度表在 `实现指南.md` 顶部，那是唯一的事实来源。**

规则范围是**完整的立直麻将**（M.LEAGUE 公式戦），不是任务书 §5 描述的简化版 ——
任务书把完整规则列为 §7 加分项，本项目把它当主线做。

---

## 硬约定

**配置、代码和目录结构都不会告诉你这些。**

- **全仓测试必须 `go test -p 1 ./...`。** `internal/store` 的用例会
  `DROP SCHEMA public CASCADE`，和别的包并行会互踩。症状是**偶发**的
  `关系 "users" 不存在`，而且**绿色的时候居多** —— 不能靠"跑一遍看看"发现。
- **`docs/开发日志.md` 是项目所有者手写的，永远不要编辑。**
- **`MajSpirit_转写.md` 只读** —— 任务书 PDF 的可靠转写（原 PDF 抽字会丢数字）。
- **范围就是 `/e/Workspace/Majspirit`。** 上级目录 `/e/Workspace` 下还有别的项目
  （`Gamejam` 是个 Godot 游戏，另有 `Kingslayer` 等），它们是**别的会话的东西**。
  上一段会话发生过一次串台：用户说"T 震击遮眼睛的代码是错的"，实际指的是 Gamejam —— 
  **动手前先确认改的是哪个仓库**，只读地看一眼不算越界，写就不行。
- **提交**：约定式提交，主题中文，**正文中文且写"为什么"** —— 取舍、实测数据、
  踩过的坑。一个阶段一个提交。
- **注释解释代码为什么长这样。** 判据是**主语**：
  `"这里必须先扫字符集，否则长度检查会盖住真问题"` → 留；
  `"我们放弃了中文密码，换来的是…"` → 搬到设计文档。
  实测数据、踩过的坑、契约留；复述代码的删。
  godoc 只认缩进代码块、`-` 列表、`[链接]`；星号和 emoji 到了那里就是噪音。
- **验证四连，每条都贴真实输出：**

  ```
  gofmt -l .        # 期望：空
  go build ./...
  go vet ./...      # 通过**不代表**能编译 —— 它不做完整检查（实测过）
  go test -p 1 ./...
  ```

## 验证的标准

**变异测试是常规动作，不是加分项。** 把被测代码改坏一处，看有没有测试挂 ——
挂了才叫有牙。覆盖率只说明"这行被执行过"。

**每条测试都要能回答"删掉它会漏掉什么缺陷"。** 答不上来就删。

三条判据的完整版在 `实现指南.md`：一个分支一行**只对输入校验成立**
（数值与规则表里每一行是独立可错的事实，不能按分支砍）；
变异本身要先确认能编译，否则构建失败会被误读成"测试没牙"；
测试行数压到源码同量级。

## 环境（这台机器上不明显的）

- PostgreSQL 端口是 **15432**（Windows 排除了 TCP 5390–5489，绑常用端口会
  `Permission denied`）。库 `majspirit` 与 `majspirit_test`，凭据在 `.env`
  （`set -a; . ./.env; set +a`）。
- **`make` 没装。Docker 没装。** 仓库里的 `Makefile` 和 `docker-compose.yml`
  现在都跑不起来 —— 所以 `Makefile` 里加的 target **只是给人看的文档**，跑不了；
  要能直接跑的命令放进 `scripts/`（见 `scripts/test-race.sh`）。
- **`-race` 可用**（2026-10-12 复核两次）。C 编译器是 Dev-Cpp 自带的 MinGW-w64 12.2
  （`D:\Program Files\Dev-Cpp\MinGW 12.2\mingw64\bin`），**已加进用户 PATH** ——
  这条在 2026-10-12 之前是假结论"没有 C 编译器"，别再信旧话。

  ```bash
  CGO_ENABLED=1 go test -p 1 -race -count=1 ./...
  ```

  新开的 Git Bash 窗口直接就能跑，**不用设 `CC`，也不用改 PATH**。
  旧会话（改 PATH 之前开的）要手动补一句
  `export PATH="/d/Program Files/Dev-Cpp/MinGW 12.2/mingw64/bin:$PATH"`。

  实测：user 包 `-race -count=1` → `ok 27.6s`；故意造一处竞态，`-race` 报
  `WARNING: DATA RACE`，去掉 `-race` 同一条测试反而绿（所以"能编译"不等于"能用"）。
  代价：比不带 `-race` 慢一个量级，别挂进日常全量跑，留给 §3.2 和 §4。

  **历史坑，仅供考古**：编译器不在 PATH 上时，`CC` 也不能直接写带空格的完整路径 ——
  Go 按空格切 `CC`，报 `cgo: C compiler "D:/Program" not found`。PATH 修好后这条不适用。
  `实现指南.md` 里那条 Docker 命令**仍未实测**（Docker 没装），现在也不必了。
- **在 PowerShell 里敲 `bash` 会开 WSL，不是 Git Bash。** `C:\Windows\System32\bash.exe`
  是 WSL 的启动器，一旦它先被解析到，命令就进了 Linux：工作区变成 `/mnt/e/Workspace/Majspirit`，
  而那里**没装 Go**，报出来的是「找不到 go」——看起来像 Windows 环境坏了，其实是另一个系统。
  实测踩过。判别用 `uname -r`：Git Bash 是 `MINGW64_NT-*`，WSL 是 `*-microsoft-standard-WSL*`。
  PowerShell 里要跑 bash 脚本，用完整路径：`& "D:\Tools\Git\bin\bash.exe" scripts/test-race.sh ./...`
  （Git 装在 `D:\Tools\Git`，不在 `C:\Program Files\Git`）。
- **PATH 已补全（2026-10-13）**：用户 PATH 加了 `C:\Program Files\Go\bin`（此前只在系统 PATH 里，
  于是 Git Bash 找不到 `go`）。改动前的备份在 `bin/PATH-user-*.txt`。

## 第三方库的坑（从代码里查不到，会重踩）

- **`coder/websocket` 的 `Conn.Close` 在有并发读者的连接上会死锁。**
  它写完关闭帧会等对端回关闭回复，而等待要拿**读锁**（`close.go` 的
  `waitCloseHandshake` → `readMu.lock`）；同一个连接上的读循环正持有读锁等数据
  （不把连接断开它就不返回）。于是关闭流程、读循环、`CloseNow` 内部的
  `waitGoroutines` 三者互等，整条连接卡死 —— goroutine 栈实测过。
  暂时性关闭用 `CloseNow`，要发关闭帧就别在有读者的连接上干等（`finishClose`
  里的写法：起 goroutine 写帧、只等很短的上限、然后 `CloseNow`）。

- **`Conn.Ping`（传输层控制帧）超时的那一刻会把整条连接关掉**：
  `setupWriteTimeout` 注册的 `AfterFunc` 里调了 `c.close()`。所以"超时之后
  再补发一条 4408 关闭帧"物理上做不到 —— 帧没地方写，客户端只看到 EOF。
  而且它绕过写 goroutine，等于第二个写者。**心跳用协议 §4.3 的应用层
  `ping`/`pong` 消息**（见 `internal/ws/client.go` 的 `heartbeat`）。
  读超时同理：`setupReadTimeout` 也会 `c.close()`，所以认证超时用显式定时器
  而不是"把带截止时间的 ctx 交给 Read"。

- 装数据库、配 Docker 加速器的步骤在 [`环境搭建.md`](docs/local/环境搭建.md)。

## 权威依据（同一个结论只写一处）

| 要查什么 | 看哪 |
| --- | --- |
| 算分规则、役种、符数 | [`docs/规则集.md`](docs/规则集.md) |
| 规则层的 API 与设计取舍 | [`docs/local/mahjong包API.md`](docs/local/mahjong包API.md) |
| 怎么做的、为什么这么选 | [`docs/local/实现指南.md`](docs/local/实现指南.md) + 分册 |
| 任务书原文 | [`MajSpirit_转写.md`](MajSpirit_转写.md)（只读） |

## 当前状态

```
阶段一  规则层      ✅   internal/mahjong，覆盖率 98.6%，纯函数
阶段二  用户系统    ✅   internal/{auth,store,user,api}
阶段三  WebSocket   ⬜   ← 下一步
阶段四  房间与对局  ⬜
阶段五  历史牌局    ⬜
阶段六  加分项      ⬜
```

依赖是单向的一条链（`api` 里没有 `import internal/store`，这条边界守着分层）：

```
cmd/server → api ──→ user ──→ store ──→ migrations
                 ↘        ↗
                   auth
```

`internal/mahjong` 是叶子：不 import 本项目任何包。所以它能脱离网络、数据库、
并发单独测试 —— 那是它 98.6% 覆盖率的来源，也是它做任何改动都不该破坏的性质。

**下一步**：读 [`docs/local/阶段三-通信.md`](docs/local/阶段三-通信.md)。
