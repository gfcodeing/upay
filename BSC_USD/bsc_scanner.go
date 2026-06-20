package BSC_USD

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"
	"upay_pro/db/sdb"
	"upay_pro/mylog"

	"go.uber.org/zap"
)

// ======================== 配置区 ========================
// 注意：以下为业务关键配置，修改前请确认链上参数。
var (
	// BSC RPC 节点列表（主用 + 备用）。callRPC 会按顺序逐个尝试，直到某个节点成功返回。
	// 若全部失败则报错，避免单节点故障导致整体漏单。
	bscRpcURLs = []string{
		// 支持 eth_getLogs 的免费节点（已验证 Docker 容器内可访问）
		"https://bsc-rpc.publicnode.com",
		"https://bsc.drpc.org",
		"https://rpc.ankr.com/bsc",
		"https://bsc-dataseed1.defibit.io",
	}
)

const (
	// BSC USDT(BEP20) 合约地址
	usdtContract = "0x55d398326f99059ff775485246999027b3197955"
	// ERC20 Transfer 事件 topic0（Transfer(address,address,uint256) 的 keccak256）
	transferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	// USDT 精度（BEP20 USDT 为 18 位）
	usdtDecimals = 18
	// BSC 出块速度（秒/块），用于把订单过期时间换算成扫描区块数
	bscBlockTime = 3
	// 扫描区块缓冲数（在按过期时间换算的基础上多扫这么多块，防止边界遗漏）
	scanBlockBuffer = 20
	// HTTP 请求超时时间（秒）
	httpTimeoutSeconds = 15
	// 金额比对容差：链上 18 位精度经 float 转换后可能有微小尾差，
	// 同时平台靠金额尾数（分）区分订单，故容差取半分（0.005）。
	amountTolerance = 0.005
)

// httpClient 带超时的 HTTP 客户端，避免节点无响应时永久阻塞扫块任务。
var httpClient = &http.Client{Timeout: httpTimeoutSeconds * time.Second}

// ======================== RPC 结构 ========================

type rpcRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type ethLog struct {
	TransactionHash string   `json:"transactionHash"`
	BlockNumber     string   `json:"blockNumber"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
}

// callRPC 按顺序尝试 bscRpcURLs 中的每个节点，任一成功即返回；全部失败返回最后一个错误。
func callRPC(method string, params []interface{}) (json.RawMessage, error) {
	body, _ := json.Marshal(rpcRequest{Jsonrpc: "2.0", Method: method, Params: params, ID: 1})

	var lastErr error
	for _, rpcURL := range bscRpcURLs {
		resp, err := httpClient.Post(rpcURL, "application/json", bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		var r rpcResponse
		if err := json.Unmarshal(raw, &r); err != nil {
			lastErr = fmt.Errorf("rpc响应解析失败(%s): %v", rpcURL, err)
			continue
		}
		if r.Error != nil {
			lastErr = fmt.Errorf("rpc error(%s): %s", rpcURL, r.Error.Message)
			continue
		}
		return r.Result, nil
	}
	return nil, fmt.Errorf("所有BSC RPC节点均失败: %v", lastErr)
}

func getLatestBlock() (int64, error) {
	result, err := callRPC("eth_blockNumber", []interface{}{})
	if err != nil {
		return 0, err
	}
	var hexBlock string
	if err := json.Unmarshal(result, &hexBlock); err != nil {
		return 0, fmt.Errorf("最新区块号解析失败: %v", err)
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(hexBlock, "0x"), 16)
	if !ok {
		return 0, fmt.Errorf("最新区块号格式异常: %s", hexBlock)
	}
	return n.Int64(), nil
}

func getBlockTimestamp(hexBlock string) (int64, error) {
	result, err := callRPC("eth_getBlockByNumber", []interface{}{hexBlock, false})
	if err != nil {
		return 0, err
	}
	var block struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(result, &block); err != nil {
		return 0, fmt.Errorf("区块信息解析失败: %v", err)
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(block.Timestamp, "0x"), 16)
	if !ok {
		return 0, fmt.Errorf("区块时间戳格式异常: %s", block.Timestamp)
	}
	return n.Int64() * 1000, nil // 秒转毫秒
}

// parseLogAmount 将一条 Transfer 日志的 Data 字段（hex 金额）解析为保留2位小数的 USDT 金额。
func parseLogAmount(data string) (float64, error) {
	val, ok := new(big.Int).SetString(strings.TrimPrefix(data, "0x"), 16)
	if !ok {
		return 0, fmt.Errorf("金额格式异常: %s", data)
	}
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(usdtDecimals), nil))
	amount, _ := new(big.Float).Quo(new(big.Float).SetInt(val), divisor).Float64()
	return math.Round(amount*100) / 100, nil
}

// scanAndMatch 扫描指定地址在订单有效期内的 USDT 入账，返回金额与时间均匹配的那一笔。
// 不再只取最新一笔，而是遍历所有入账记录，避免同一地址在有效期内收到多笔时漏判真实付款。
// 找到匹配返回 matched=true，否则 matched=false。
func scanAndMatch(order sdb.Orders) (txHash string, amount float64, timestampMs int64, matched bool, err error) {
	// 零地址防御：order.Token 为空会导致 paddedTo 变成零地址，
	// 而合约 mint/burn 等操作确实会产生流向零地址的 Transfer，可能被误判入账，故直接拒绝。
	toAddress := strings.TrimSpace(order.Token)
	if toAddress == "" {
		err = fmt.Errorf("订单收款地址为空，拒绝扫块")
		return
	}

	latest, err := getLatestBlock()
	if err != nil {
		return
	}
	// 根据动态配置的订单过期时间换算扫描区块数，多扫 scanBlockBuffer 块做缓冲
	expirationSeconds := int64(sdb.GetSetting().ExpirationDate.Seconds())
	scanBlockRange := expirationSeconds/bscBlockTime + scanBlockBuffer
	fromBlock := latest - scanBlockRange
	if fromBlock < 0 {
		fromBlock = 0
	}

	// topic2: to 地址左补零到 32 字节
	paddedTo := "0x000000000000000000000000" + strings.TrimPrefix(strings.ToLower(toAddress), "0x")

	params := map[string]interface{}{
		"address":   usdtContract,
		"topics":    []interface{}{transferTopic, nil, paddedTo},
		"fromBlock": fmt.Sprintf("0x%x", fromBlock),
		"toBlock":   "latest",
	}
	result, err := callRPC("eth_getLogs", []interface{}{params})
	if err != nil {
		return
	}

	var logs []ethLog
	if err = json.Unmarshal(result, &logs); err != nil {
		err = fmt.Errorf("日志解析失败: %v", err)
		return
	}
	if len(logs) == 0 {
		return
	}

	// 倒序遍历（最新优先），返回第一笔金额与时间窗口都匹配的入账。
	for i := len(logs) - 1; i >= 0; i-- {
		l := logs[i]
		if l.TransactionHash == "" {
			continue
		}
		amt, perr := parseLogAmount(l.Data)
		if perr != nil {
			mylog.Logger.Warn("BSC-USDT 跳过一笔金额解析失败的日志", zap.String("txHash", l.TransactionHash), zap.Error(perr))
			continue
		}
		// 金额容差比较，避免 float 尾差导致漏判
		if math.Abs(amt-order.ActualAmount) >= amountTolerance {
			continue
		}
		ts, terr := getBlockTimestamp(l.BlockNumber)
		if terr != nil {
			mylog.Logger.Warn("BSC-USDT 跳过一笔区块时间获取失败的日志", zap.String("txHash", l.TransactionHash), zap.Error(terr))
			continue
		}
		// 时间窗口校验：必须落在订单开始与过期之间（含边界）
		if ts < order.StartTime || ts > order.ExpirationTime {
			continue
		}
		return l.TransactionHash, amt, ts, true, nil
	}

	return
}

func Start_scan(order sdb.Orders) bool {
	mylog.Logger.Info("正在通过扫区块获取BSC-USDT 入账方向的交易数据...")

	txHash, amount, timestampMs, matched, err := scanAndMatch(order)
	if err != nil {
		mylog.Logger.Error("扫块查询失败", zap.Error(err))
		return false
	}

	if !matched {
		mylog.Logger.Info("BSC-USDT 未发现符合本次交易的入账记录",
			zap.Float64("期望金额", order.ActualAmount),
			zap.Int64("订单开始", order.StartTime), zap.Int64("订单过期", order.ExpirationTime))
		return false
	}

	mylog.Logger.Info(fmt.Sprintf("BSC-USDT 交易记录符合本次交易验证: amount=%.2f txHash=%s time=%s，接下来更新数据库",
		amount, txHash, time.UnixMilli(timestampMs).Format("2006-01-02 15:04:05")))

	// 原子入账：带状态守卫 + txHash 唯一去重，禁止裸 Save 全字段覆盖
	return sdb.MarkOrderPaid(order.TradeId, txHash)
}
