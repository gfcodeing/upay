package trx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"upay_pro/db/sdb"
	"upay_pro/mylog"

	"go.uber.org/zap"
)

// Transaction response structures
type TransactionResponse struct {
	Data    []Transaction `json:"data"`
	Success bool          `json:"success"`
	Meta    Meta          `json:"meta"`
}

type Transaction struct {
	Ret              []Ret         `json:"ret"`
	Signature        []string      `json:"signature"`
	TxID             string        `json:"txID"`
	NetUsage         int           `json:"net_usage"`
	RawDataHex       string        `json:"raw_data_hex"`
	NetFee           int           `json:"net_fee"`
	EnergyUsage      int           `json:"energy_usage"`
	BlockNumber      int64         `json:"blockNumber"`
	BlockTimestamp   int64         `json:"block_timestamp"`
	EnergyFee        int           `json:"energy_fee"`
	EnergyUsageTotal int           `json:"energy_usage_total"`
	RawData          RawData       `json:"raw_data"`
	InternalTxs      []interface{} `json:"internal_transactions"`
}

type Ret struct {
	ContractRet string `json:"contractRet"`
	Fee         int    `json:"fee"`
}

type RawData struct {
	Contract      []Contract `json:"contract"`
	RefBlockBytes string     `json:"ref_block_bytes"`
	RefBlockHash  string     `json:"ref_block_hash"`
	Expiration    int64      `json:"expiration"`
	Timestamp     int64      `json:"timestamp"`
}

type Contract struct {
	Parameter Parameter `json:"parameter"`
	Type      string    `json:"type"`
}

type Parameter struct {
	Value   Value  `json:"value"`
	TypeURL string `json:"type_url"`
}

type Value struct {
	Amount       float64 `json:"amount"`
	OwnerAddress string  `json:"owner_address"`
	ToAddress    string  `json:"to_address"`
}

type Meta struct {
	At          int64  `json:"at"`
	Fingerprint string `json:"fingerprint"`
	Links       Links  `json:"links"`
	PageSize    int    `json:"page_size"`
}

type Links struct {
	Next string `json:"next"`
}

// API配置常量
const (
	TronGridBaseURL = "https://api.trongrid.io/v1"

	DefaultLimit   = 1
	DefaultTimeout = 30 * time.Second
)

// API查询参数配置
type QueryConfig struct {
	Account       string
	Limit         int
	OnlyConfirmed bool
	OnlyTo        bool
	MinTimestamp  int64
	MaxTimestamp  int64
	Fingerprint   string
}

// 构建API URL
func buildAPIURL(config QueryConfig) string {
	baseURL := fmt.Sprintf("%s/accounts/%s/transactions?limit=%d&only_confirmed=%t&only_to=%t&min_timestamp=%d&max_timestamp=%d", TronGridBaseURL, config.Account, config.Limit, config.OnlyConfirmed, config.OnlyTo, config.MinTimestamp, config.MaxTimestamp)

	return baseURL
}

func Start2(order sdb.Orders) bool {

	mylog.Logger.Info("第二个TRX_TronGrid开始查询", zap.String("order_id", order.TradeId))

	// 配置API查询参数
	config := QueryConfig{
		Account:       order.Token,
		Limit:         DefaultLimit,
		OnlyConfirmed: true,
		OnlyTo:        true,
		// MinTimestamp 和 MaxTimestamp 可以根据需要设置
		MinTimestamp: order.StartTime,      // 订单开始时的时间戳
		MaxTimestamp: order.ExpirationTime, // 订单过期时间戳
	}

	// 构建API URL
	apiURL := buildAPIURL(config)
	fmt.Printf("请求URL: %s\n\n", apiURL)

	// 创建HTTP客户端
	client := &http.Client{
		Timeout: DefaultTimeout,
	}

	// 创建请求并设置请求头
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		mylog.Logger.Error("TRX_TronGrid创建请求失败", zap.Error(err))
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("TRON-PRO-API-KEY", sdb.GetApiKey().Trongrid)

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		// log.Fatalf("请求失败: %v", err)
		mylog.Logger.Error("TRX_TronGrid发送请求失败", zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		// log.Fatalf("API请求失败，状态码: %d", resp.StatusCode)
		mylog.Logger.Error("TRX_TronGrid返回状态码不是200", zap.Int("status_code", resp.StatusCode))
		return false
	}

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// log.Fatalf("读取响应失败: %v", err)
		mylog.Logger.Error("TRX_TronGrid读取响应失败", zap.Error(err))
		return false
	}

	// 解析JSON响应
	var txResponse TransactionResponse
	err = json.Unmarshal(body, &txResponse)
	if err != nil {
		// log.Fatalf("JSON解析失败: %v", err)
		mylog.Logger.Error("TRX_TronGrid JSON解析失败", zap.Error(err))
		return false
	}

	// 检查 API 响应的 Success 字段

	if len(txResponse.Data) > 0 {

		tx := txResponse.Data[0]

		// 检查 API 响应的 Success 字段
		if tx.Ret[0].ContractRet != "SUCCESS" {
			mylog.Logger.Error("TRX_TronGrid 没有返回SUCCESS", zap.String("contractRet", tx.Ret[0].ContractRet))
			return false
		}

		if len(tx.RawData.Contract) > 0 {
			contract := tx.RawData.Contract[0]

			// 获取转账金额
			amount := formatAmount(contract.Parameter.Value.Amount)

			if amount == order.ActualAmount {
				// 原子入账：带状态守卫 + txHash 唯一去重，禁止裸 Save 全字段覆盖
				return sdb.MarkOrderPaid(order.TradeId, tx.TxID)
			}
			mylog.Logger.Info("TRX_TronGrid已经查询到转账记录，但是金额不符合要求")
			return false
		}
		return false
	}

	return false
}
