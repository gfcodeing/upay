package USDC_ERC20

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"upay_pro/db/sdb"
	"upay_pro/mylog"

	"go.uber.org/zap"
)

// EtherscanConfig API配置结构体
type EtherscanConfig struct {
	BaseURL         string
	ChainID         int
	Module          string
	Action          string
	Address         string
	ContractAddress string
	APIKey          string
	Page            int
	Offset          int
	Sort            string
}

// EtherscanResponse Etherscan API响应结构体
type EtherscanResponse struct {
	Status  string             `json:"status"`
	Message string             `json:"message"`
	Result  []TokenTransaction `json:"result"`
}

// TokenTransaction 代币交易结构体
type TokenTransaction struct {
	BlockNumber       string `json:"blockNumber"`
	TimeStamp         string `json:"timeStamp"`
	Hash              string `json:"hash"`
	Nonce             string `json:"nonce"`
	BlockHash         string `json:"blockHash"`
	From              string `json:"from"`
	ContractAddress   string `json:"contractAddress"`
	To                string `json:"to"`
	Value             string `json:"value"`
	TokenName         string `json:"tokenName"`
	TokenSymbol       string `json:"tokenSymbol"`
	TokenDecimal      string `json:"tokenDecimal"`
	TransactionIndex  string `json:"transactionIndex"`
	Gas               string `json:"gas"`
	GasPrice          string `json:"gasPrice"`
	GasUsed           string `json:"gasUsed"`
	CumulativeGasUsed string `json:"cumulativeGasUsed"`
	Input             string `json:"input"`

	MethodID      string `json:"methodId"`
	FunctionName  string `json:"functionName"`
	Confirmations string `json:"confirmations"`
}

// buildAPIURL 构建API请求URL
func (config *EtherscanConfig) buildAPIURL() string {
	params := url.Values{}
	params.Add("chainid", strconv.Itoa(config.ChainID))
	params.Add("module", config.Module)
	params.Add("action", config.Action)
	params.Add("address", config.Address)
	params.Add("contractAddress", config.ContractAddress)
	params.Add("apikey", config.APIKey)
	params.Add("page", strconv.Itoa(config.Page))
	params.Add("offset", strconv.Itoa(config.Offset))
	params.Add("sort", config.Sort)

	return config.BaseURL + "?" + params.Encode()
}

func Start(order sdb.Orders) bool {
	// API配置 - 可以方便地修改各个参数
	config := &EtherscanConfig{
		BaseURL:         "https://api.etherscan.io/v2/api",
		ChainID:         1,                                            // 以太坊主网
		Module:          "account",                                    // 账户模块
		Action:          "tokentx",                                    // 查询代币交易
		Address:         order.Token,                                  // 查询的地址
		ContractAddress: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", // USDC合约地址
		APIKey:          sdb.GetApiKey().Etherscan,
		Page:            1,      // 分页参数
		Offset:          1,      // 返回记录数
		Sort:            "desc", // 排序方式,desc 降序 最新的在最前面,asc 升序 最旧的在最前面
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 构建API URL
	apiURL := config.buildAPIURL()
	fmt.Println("请求URL:", apiURL)

	// 发送请求
	resp, err := client.Get(apiURL)
	if err != nil {
		mylog.Logger.Error("ERC20-USDT请求失败", zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	fmt.Println("Status Code:", resp.StatusCode)

	// 读取响应数据
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		mylog.Logger.Error("读取响应数据失败", zap.Error(err))
		return false
	}

	// 解析为结构化数据
	var etherscanResp EtherscanResponse
	err = json.Unmarshal(data, &etherscanResp)
	if err != nil {
		mylog.Logger.Error("JSON解析错误", zap.Error(err))
		return false
	}

	if etherscanResp.Message != "OK" || len(etherscanResp.Result) == 0 {
		mylog.Logger.Error("API返回消息错误", zap.String("message", etherscanResp.Message), zap.Int("结果切片", len(etherscanResp.Result)))
		return false
	}

	// 时间戳转为毫秒
	timeStamp, err := strconv.ParseInt(etherscanResp.Result[0].TimeStamp, 10, 64)
	if err != nil {
		mylog.Logger.Error("时间戳转换失败", zap.Error(err))
		return false
	}
	timeStampMs := timeStamp * 1000

	// 格式化金额数字

	amount := formatAmount(etherscanResp.Result[0].Value)

	// 符合条件就更新数据库

	if strings.EqualFold(etherscanResp.Result[0].To, order.Token) && timeStampMs > order.StartTime && timeStampMs < order.ExpirationTime && amount == order.ActualAmount && etherscanResp.Result[0].Hash != "" && etherscanResp.Result[0].TokenSymbol == "USDC" {
		// 原子入账：带状态守卫 + txHash 唯一去重，禁止裸 Save 全字段覆盖
		return sdb.MarkOrderPaid(order.TradeId, etherscanResp.Result[0].Hash)
	}
	mylog.Logger.Info("USDC_ERC20 找到的记录不满足要求，当前找的交易记录：", zap.Any("HASH", etherscanResp.Result[0].Hash), zap.Any("金额", amount), zap.Any("时间戳格式化后：", time.Unix(timeStamp, 0).Format("2006-01-02 15:04:05")))
	return false
}

func formatAmount(quant string) float64 {
	// 直接将字符串转为 float64 类型
	amount, err := strconv.ParseFloat(quant, 64)
	if err != nil {
		// log.Printf("Error parsing amount: %v", err)
		mylog.Logger.Error("Error parsing amount", zap.Any("error", err))
		return 0 // 如果转换失败，返回 0
	}

	// 使用 1e6 计算金额，转换为 float64 类型
	amountFloat := amount / 1e6 // 使用 1e6 来处理精度

	// 保留小数点后2位
	amountFloat = math.Round(amountFloat*100) / 100

	return amountFloat
}
