package tron

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"upay_pro/db/sdb"
	"upay_pro/mylog"

	"go.uber.org/zap"
)

/* type TransferDetails struct {
	TokenAbbr     string
	TransactionID string
	Quant         float64
	FromAddress   string
	ToAddress     string
	FinalResult   string // 可以用来表示 API 请求是否成功
} */

// --- 为解析 JSON 响应定义的结构体 ---
type TokenInfo struct {
	Symbol   string `json:"symbol"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
	Name     string `json:"name"`
}

type TransactionData struct {
	TransactionID  string    `json:"transaction_id"`
	TokenInfo      TokenInfo `json:"token_info"`
	BlockTimestamp int64     `json:"block_timestamp"`
	From           string    `json:"from"`
	To             string    `json:"to"`
	Type           string    `json:"type"`
	Value          string    `json:"value"`
}

type Meta struct {
	At          int64             `json:"at"`
	Fingerprint string            `json:"fingerprint"`
	Links       map[string]string `json:"links"`
	PageSize    int               `json:"page_size"`
}

type ApiResponseGrid struct {
	Data    []TransactionData `json:"data"`
	Success bool              `json:"success"`
	Meta    Meta              `json:"meta"`
}

// --- 结束 JSON 结构体定义 ---

// 传入钱包地址
// 注意：startTime 和 endTime 参数当前未在此函数实现中使用
func GetTransactionsGrid(order sdb.Orders) bool {

	// 1. 构造请求 URL
	// 注意：这里硬编码了合约地址和 limit=1，根据需要可以将其作为参数传入
	contractAddress := "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t" // USDT TRC20 合约地址
	limit := 1
	min_timestamp := order.StartTime
	max_timestamp := order.ExpirationTime

	apiURL := fmt.Sprintf("https://api.trongrid.io/v1/accounts/%s/transactions/trc20?contract_address=%s&limit=%d&only_confirmed=true&min_block_timestamp=%v&max_block_timestamp=%v",
		order.Token, contractAddress, limit, min_timestamp, max_timestamp)

	// 创建HTTP客户端
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	// 创建请求并设置请求头
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		mylog.Logger.Error("USDT_TronGrid创建请求失败", zap.Error(err))
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("TRON-PRO-API-KEY", sdb.GetApiKey().Trongrid)

	// 2. 发送 HTTP GET 请求
	resp, err := client.Do(req)
	if err != nil {
		mylog.Logger.Error("USDT_TronGrid发送请求失败", zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	// 3. 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		mylog.Logger.Error("USDT_TronGrid读取响应体失败", zap.Error(err))
		return false
	}

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		mylog.Logger.Error("USDT_TronGrid请求失败", zap.Int("statusCode", resp.StatusCode))
		return false
	}

	// 4. 解析 返回的JSON 数据到结构体
	var apiResponse ApiResponseGrid
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		mylog.Logger.Error("USDT_TronGrid解析 JSON 失败", zap.Error(err))
		return false
	}
	if len(apiResponse.Data) > 0 {
		// 已经查到数据
		// 金额转换
		amount := formatAmount(apiResponse.Data[0].Value)

		if amount == order.ActualAmount && apiResponse.Data[0].TransactionID != "" && apiResponse.Data[0].TokenInfo.Symbol == "USDT" && strings.EqualFold(apiResponse.Data[0].To, order.Token) && strings.EqualFold(apiResponse.Data[0].Type, "Transfer") {

			// 原子入账：带状态守卫 + txHash 唯一去重，禁止裸 Save 全字段覆盖
			return sdb.MarkOrderPaid(order.TradeId, apiResponse.Data[0].TransactionID)

		}
		mylog.Logger.Error("TronGrid 获取的转账记录不满足要求")
		return false
	}
	mylog.Logger.Info("TronGrid没有查询到转账记录")
	return false

}

/* func formatAmount(quant string) float64 {
	// 直接将字符串转为 float64 类型
	amount, err := strconv.ParseFloat(quant, 64)
	if err != nil {
		log.Printf("Error parsing amount: %v", err)
		return 0 // 如果转换失败，返回 0
	}

	// 使用 1e6 计算金额，转换为 float64 类型
	amountFloat := amount / 1e6 // 使用 1e6 来处理精度

	// 保留小数点后2位
	amountFloat = math.Round(amountFloat*100) / 100

	return amountFloat
}
*/
