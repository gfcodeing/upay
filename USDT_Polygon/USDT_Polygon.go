package USDT_Polygon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"upay_pro/db/sdb"
	"upay_pro/mylog"

	"go.uber.org/zap"
)

// POLYGONClient Polygon API客户端
type POLYGONClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewPOLYGONClient 创建新的Polygon客户端
func NewPOLYGONClient(apiKey string) *POLYGONClient {
	return &POLYGONClient{
		apiKey:  apiKey,
		baseURL: "https://api.etherscan.io/v2/api",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *POLYGONClient) GetTransfers(contractAddress, walletAddress string) (*ApiResponse, error) {
	url := fmt.Sprintf("%s?chainid=137&module=account&action=tokentx&page=1&sort=desc&contractaddress=%s&address=%s&offset=1&apikey=%s",
		c.baseURL, contractAddress, walletAddress, c.apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var apiResponse ApiResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	if apiResponse.Status != "1" {
		return nil, fmt.Errorf("API返回错误: %s", apiResponse.Message)
	}

	return &apiResponse, nil
}

func Start(order sdb.Orders) bool {
	apiKey := sdb.GetApiKey().Etherscan
	// USDT的合约地址：0xc2132D05D31c914a87C6611C10748AEb04B58e8F
	contractAddress := "0xc2132D05D31c914a87C6611C10748AEb04B58e8F"
	walletAddress := order.Token // 使用订单中的钱包地址

	polygonClient := NewPOLYGONClient(apiKey)
	txs, err := polygonClient.GetTransfers(contractAddress, walletAddress)
	if err != nil {
		// log.Printf("查询USDT交易失败: %v", err)
		mylog.Logger.Error("查询USDT-Polygon交易失败", zap.Error(err))
		return false
	}

	if len(txs.Result) == 0 {
		// 检查是否存在转账记录
		return false
	}

	// 获取最新的交易记录
	latestTx := txs.Result[0]

	// 转换时间戳为毫秒
	// 字符串转为int64
	timeStamp, err := strconv.ParseInt(latestTx.TimeStamp, 10, 64)
	if err != nil {
		log.Printf("时间戳转换失败: %v", err)
		return false
	}
	// 转换为毫秒级时间戳，因为原来是秒级时间戳
	timeStampMs := timeStamp * 1000

	// 转换金额
	amount, err := formatAmount(latestTx.Value)
	if err != nil {
		log.Printf("金额转换失败: %v", err)
		return false
	}

	// 验证交易条件
	if latestTx.Hash != "" &&
		latestTx.TokenSymbol == "USDT0" &&
		timeStampMs > order.StartTime &&
		timeStampMs < order.ExpirationTime &&
		amount == order.ActualAmount && // 使用浮点数比较
		strings.EqualFold(latestTx.To, walletAddress) {

		// 如果在指定时间内，并且金额正确，并且交易Hash不为空，则说明已经入账成功，可以更新数据库

		// 原子入账：带状态守卫 + txHash 唯一去重，禁止裸 Save 全字段覆盖
		return sdb.MarkOrderPaid(order.TradeId, latestTx.Hash)
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
	amountFloat := amount / 1e6

	// 保留小数点后2位
	amountFloat = math.Round(amountFloat*100) / 100

	return amountFloat, nil
}

type TokenTx struct {
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

type ApiResponse struct {
	Status  string    `json:"status"`
	Message string    `json:"message"`
	Result  []TokenTx `json:"result"`
}
