package USDC_BSC

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

// Transaction 表示单个交易记录
type Transaction struct {
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
	MethodId          string `json:"methodId"`
	FunctionName      string `json:"functionName"`
	Confirmations     string `json:"confirmations"`
}

// APIResponse 表示API响应结构
type APIResponse struct {
	Status  string        `json:"status"`
	Message string        `json:"message"`
	Result  []Transaction `json:"result"`
}

// APIConfig 包含API请求的配置参数
type APIConfig struct {
	BaseURL         string
	ChainID         string
	Module          string
	Action          string
	Address         string
	ContractAddress string
	APIKey          string
	Page            string
	Offset          string
	Sort            string
	ProxyURL        string // 代理服务器地址
}

// NewDefaultConfig 创建默认配置
func NewDefaultConfig() *APIConfig {
	return &APIConfig{
		BaseURL: "https://api.etherscan.io/v2/api",
		ChainID: "56",
		Module:  "account",
		Action:  "tokentx",
		// USDC合约地址：0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d
		ContractAddress: "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d",
		APIKey:          sdb.GetApiKey().Etherscan,
		Page:            "1",
		Offset:          "1",
		Sort:            "desc",
		// ProxyURL:        "http://127.0.0.1:7890", // 默认本机代理
	}
}

// BuildURL 构建完整的API请求URL
func (c *APIConfig) BuildURL() string {
	return fmt.Sprintf("%s?chainid=%s&module=%s&action=%s&address=%s&contractAddress=%s&apikey=%s&page=%s&offset=%s&sort=%s",
		c.BaseURL, c.ChainID, c.Module, c.Action, c.Address, c.ContractAddress, c.APIKey, c.Page, c.Offset, c.Sort)
}

// fetchBSCUSDTransactions 请求BSC-USD代币交易数据
func fetchBSCUSDTransactions(config *APIConfig) (*APIResponse, error) {
	apiURL := config.BuildURL()
	fmt.Printf("请求URL: %s\n", apiURL)

	// 创建HTTP客户端，设置超时和代理
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 如果配置了代理，则设置代理
	if config.ProxyURL != "" {
		proxyURL, err := url.Parse(config.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("代理URL解析失败: %v", err)
		}
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
		fmt.Printf("使用代理: %s\n", config.ProxyURL)
	}

	// 发送GET请求
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP错误: %d", resp.StatusCode)
	}

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	// 解析JSON
	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %v", err)
	}

	return &apiResp, nil
}

func Start(order sdb.Orders) bool {
	// fmt.Println("正在获取BSC-USD交易数据...")

	// 创建配置
	config := NewDefaultConfig()
	config.Address = order.Token
	// 可以根据需要自定义配置
	// config.APIKey = "your_api_key_here"
	// config.Address = "your_address_here"
	// config.ProxyURL = "" // 如果不需要代理，设置为空字符串

	// 调用API获取数据
	data, err := fetchBSCUSDTransactions(config)
	if err != nil {
		// fmt.Printf("查询BSC-USDT交易失败: %v\n", err)
		mylog.Logger.Error("查询BSC-USDT交易失败", zap.Error(err))
		return false
	}
	if data.Status == "1" && len(data.Result) > 0 {
		// 将记录中的时间由秒转为毫秒时间戳
		timeStamp, err := strconv.ParseInt(data.Result[0].TimeStamp, 10, 64)
		if err != nil {
			fmt.Printf("时间戳转换失败: %v\n", err)
			return false
		}
		timeStampMs := timeStamp * 1000

		// 转换金额
		amount, err := formatAmount(data.Result[0].Value)
		if err != nil {
			mylog.Logger.Info("金额转换失败", zap.Error(err))
			return false
		}

		if data.Result[0].Hash != "" && data.Result[0].TokenSymbol == "USDC" && timeStampMs > order.StartTime && timeStampMs < order.ExpirationTime && amount == order.ActualAmount && strings.EqualFold(data.Result[0].To, order.Token) {
			// 如果在指定时间内，并且金额正确，并且交易Hash不为空，则说明已经入账成功，可以更新数据库

			// 原子入账：带状态守卫 + txHash 唯一去重，禁止裸 Save 全字段覆盖
			return sdb.MarkOrderPaid(order.TradeId, data.Result[0].Hash)
		}
		return false

	}
	return false

}

func formatAmount(amountStr string) (float64, error) {
	// 将字符串转为 float64 类型
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return 0, fmt.Errorf("金额转换失败: %w", err)
	}

	// 使用 1e6 计算金额，转换为 float64 类型 (USDT有6位小数)
	amountFloat := amount / 1e18

	// 保留小数点后2位
	amountFloat = math.Round(amountFloat*100) / 100

	return amountFloat, nil
}
