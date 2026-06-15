# BSC (BEP20) USDT / USDC 扫块入账逻辑修复说明

> 日期：2026-06-15
> 涉及文件：`BSC_USD/bsc_scanner.go`、`USDC_BSC/usdc_bsc_scanner.go`
> 背景：上一版改动（用 `eth_getLogs` 直连 RPC 替换第三方 scanner 库）解决了"查不到支付订单"的问题，但在资产安全上引入了误判 / 漏判风险。本次修复在保留新扫块方式的前提下，补齐资产安全校验。

---

## 一、调用链

定时任务 `cron.UsdtCheckJob.Run()` 遍历所有 `StatusWaitPay`（等待支付）订单：

- `USDT-BSC`：先 `BSC_USD.Start`（Etherscan API 方式），失败再 `BSC_USD.Start_scan`（本文件，扫块兜底）
- `USDC-BSC`：先 `USDC_BSC.Start`（API），失败再 `USDC_BSC.Start_scan`（扫块兜底）

`Start_scan` 命中后把订单置为 `sdb.StatusPaySuccess` 并写库，随后触发回调。**因为直接决定资产是否入账，校验必须严谨。**

---

## 二、本次修复的问题清单

| 编号 | 级别 | 问题 | 修复方式 |
|------|------|------|----------|
| 1 | 🔴 严重 | 收款地址为空时 `paddedTo` 退化为零地址，合约 mint/burn 的 Transfer 可能被误判入账 | `scanAndMatch` 入口校验 `order.Token` 去空格后非空，否则直接返回错误拒绝扫块 |
| 2 | 🔴 严重 | 只取最新一笔日志 `logs[len-1]` 与订单金额比对；同一地址有效期内收到多笔时，真实付款会漏判 | 改为**倒序遍历全部日志**，返回第一笔金额容差匹配且时间窗口匹配的入账 |
| 3 | 🟠 中等 | `SetString` / `json.Unmarshal` 返回值被丢弃，异常输入静默算成 0 | 全部补 `ok` / `err` 判断并返回错误；单笔解析失败用 `Logger.Warn` 记录并跳过该笔 |
| 4 | 🟠 中等 | 金额用 `==` 比较 float64，18 位精度转换后尾差可能导致漏判 | 改为容差比较 `math.Abs(amt-ActualAmount) < amountTolerance`（0.005） |
| 5 | 🟢 配置 | 单一公共 RPC 节点、`http.Post` 无超时、无重试 | 抽取多节点列表 `bscRpcURLs`，带超时的 `httpClient`，`callRPC` 顺序故障转移 |

---

## 三、配置项（已抽取到文件顶部「配置区」，含中文注释）

两个文件配置一致，区别仅在合约地址与币种精度常量名：

| 配置项 | 含义 | 默认值 |
|--------|------|--------|
| `bscRpcURLs` | BSC RPC 节点列表，按序故障转移 | 4 个公共节点 |
| `usdtContract` / `usdcContract` | BEP20 合约地址 | USDT `0x55d3...7955` / USDC `0x8AC7...580d` |
| `transferTopic` | ERC20 `Transfer` 事件 topic0 | keccak256 固定值 |
| `usdtDecimals` / `usdcDecimals` | 代币精度 | 18 |
| `bscBlockTime` | 出块速度（秒/块），用于过期时间→区块数换算 | 3 |
| `scanBlockBuffer` | 扫描区块缓冲数 | 20 |
| `httpTimeoutSeconds` | HTTP 超时（秒） | 15 |
| `amountTolerance` | 金额比对容差 | 0.005（半分） |

> 扫描区块数 = 订单过期秒数 / `bscBlockTime` + `scanBlockBuffer`，过期时间取自 `sdb.GetSetting().ExpirationDate`（动态配置，默认 10 分钟）。

---

## 四、核心匹配逻辑（`scanAndMatch`）

一笔链上 Transfer 被认定为「本次订单的付款」需**同时满足**：

1. `order.Token`（收款地址）非空 —— 零地址防御
2. RPC 端按 `address=合约`、`topics=[Transfer, *, paddedTo]` 过滤（等价"转账类型为 Transfer 且收款方为本订单地址"）
3. 金额容差匹配：`|链上金额 - order.ActualAmount| < 0.005`
4. 时间窗口：`order.StartTime < 区块时间 < order.ExpirationTime`
5. `transactionHash` 非空

倒序遍历（最新优先），命中第一笔即返回；解析失败的单笔仅 Warn 跳过，不影响其余。

状态统一使用枚举 `sdb.StatusPaySuccess`，未写死。

---

## 五、已知遗留 / 后续可选优化

- **未等待区块确认数**：`toBlock: "latest"` 包含未最终确认区块，BSC 重组概率极低，暂未处理。若需更稳，可改为 `latest - N` 并加 `confirmBlocks` 配置项。
- **`eth_getLogs` 单次区块跨度**：默认 10 分钟过期约扫 220 块，公共节点一般支持；若调大过期时间需注意节点对区块跨度的上限。
- **编译验证**：本次修改在无 Go 环境的机器上完成，**尚未执行 `go build` 验证**，需在带 Go SDK 的环境（如 GoLand）确认编译通过。

---

## 六、并发与攻击面评估（2026-06-15 追加）

### 扫块逻辑本身（本次改动）
- 定时任务 `cron.go` 用 `cron.SkipIfStillRunning` + `@every 2s`，同一 `UsdtCheckJob` 不会并发执行，`Start_scan` 不会被并发调用到同一订单。
- `scanAndMatch` 全部使用局部变量，`httpClient`（`*http.Client`）并发安全，多订单并行扫块无数据竞争。
- 攻击面：零地址防御 + RPC 端 topics 过滤 + 本地金额/时间双重校验；数据源为 BSC 节点而非用户输入，无法伪造日志骗过校验。比旧版更安全。

### 隐患 A（已修复）：金额占用的 TOCTOU 竞态
- **位置**：`web/function.go` 下单流程。
- **原问题**：用 `getRedisAmount`（`EXISTS`）+ `rdb.RDB.Set` 两步占用「钱包地址+金额」，非原子。并发下单算出相同尾数金额时可能同时通过检查，导致两个订单拥有相同 `Token+ActualAmount`，一笔真实付款被两个订单同时判为入账（重复发货/重复回调）。
- **修复**：改用 Redis 原子命令 `SetNX`（SET if Not eXists）一步完成「检查+占用」，抢占失败则递增金额换下一个尾数。`getRedisAmount` 不再被下单流程调用（保留供只读场景）。

### 隐患 B（已修复）：回调协程幂等仅靠后置检查
- **位置**：`cron.go` 命中后 `go ProcessCallback(v)`，存在多个调用点（各币种扫块 + `web.go` 手动触发）。
- **原问题**：`ProcessCallback` 开头检查 `CallBackConfirm` 做幂等，能挡住大部分重复。但订单置成功到回调确认之间存在时间窗，`SkipIfStillRunning` 只防 cron job 重叠、不防同一订单跨两轮各 `go` 出一个回调协程。高压下仍有薄弱窗口，可能重复回调。
- **修复**：在 `ProcessCallback` 内部（重新查询最新订单、确认已支付之后）加 Redis 分布式锁：
  - 二次幂等：用最新订单的 `CallBackConfirm` 再判一次（入参 `v` 是快照可能过期）。
  - `SetNX` 抢锁 `callback_lock:{tradeId}`，抢不到说明已有协程在处理，直接返回。
  - 锁 TTL = `callbackLockTTL`（60s，覆盖最多 5 次 × 5s 重试），处理结束 `defer` 释放，使回调失败的订单仍能被下一轮 cron 重试。
  - 因加在 `ProcessCallback` 内部，所有调用点统一覆盖。
- **配置项**（`cron.go` 配置区）：`callbackLockKeyPrefix`（锁 key 前缀）、`callbackLockTTL`（锁过期时间）。

### 隐患 C（已修复）：过期任务与支付成功的写覆盖竞态
- **位置**：`mq/mq.go` 过期任务 `handleCheckStatusCodeTask`。
- **原问题**：`First` 读出整条订单 → 内存判断 `if Status==WaitPay` → `Save(&order)` 全字段覆盖。读和写之间若扫块协程刚把订单改为 `PaySuccess`，旧快照的 `Save` 会把状态覆盖回 `Expired`，导致已付款订单被误判过期（资产风险）。
- **修复**：改为带条件的原子 UPDATE，把状态判定下推到 SQL 的 `WHERE`，由数据库保证原子性：
  ```go
  sdb.DB.Model(&sdb.Orders{}).
      Where("trade_id = ? AND status = ?", payload, sdb.StatusWaitPay).
      Update("status", sdb.StatusExpired)
  ```
  仅当当前仍为「等待支付」时才会被改成「已过期」；`RowsAffected==0` 说明订单已支付成功或已是其他状态，跳过过期处理。同时避免了全字段覆盖对其他字段的副作用。

