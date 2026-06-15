package tron

import (
	"encoding/json" // 导入 JSON 编码/解码包
	"fmt"           // 导入 fmt 包用于格式化输出
	"io"            // 导入 io 包用于读取响应体
	"strings"
	"time"

	// 导入 log 包用于记录日志
	"math"
	"net/http" // 导入 http 包用于发起 HTTP 请求
	"net/url"  // 导入 url 包用于构建请求的 URL
	"strconv"
	"upay_pro/db/sdb"
	"upay_pro/mylog"

	"go.uber.org/zap"
	// 用于处理时间和日期的 Go 语言库
)

// 定义 TokenTransfer 结构体用于解析每个转账记录
type TokenTransfer struct {
	TransactionID   string                 `json:"transaction_id"`   // 交易 ID
	Status          int                    `json:"status"`           // 状态
	BlockTS         int64                  `json:"block_ts"`         // 区块时间戳
	FromAddress     string                 `json:"from_address"`     // 发送者地址
	FromAddressTag  map[string]interface{} `json:"from_address_tag"` // 发送者标签
	ToAddress       string                 `json:"to_address"`       // 接收者地址
	ToAddressTag    map[string]interface{} `json:"to_address_tag"`   // 接收者标签
	Block           int                    `json:"block"`            // 区块号
	ContractAddress string                 `json:"contract_address"` // 合约地址
	Quant           string                 `json:"quant"`            // 转账数量
	Confirmed       bool                   `json:"confirmed"`        // 是否确认
	ContractRet     string                 `json:"contractRet"`      // 合约返回
	FinalResult     string                 `json:"finalResult"`      // 最终结果
	Revert          bool                   `json:"revert"`           // 是否回滚
	TokenInfo       struct {
		TokenID      string `json:"tokenId"`      // 代币 ID
		TokenAbbr    string `json:"tokenAbbr"`    // 代币符号
		TokenName    string `json:"tokenName"`    // 代币名称
		TokenDecimal int    `json:"tokenDecimal"` // 代币小数位数
		TokenCanShow int    `json:"tokenCanShow"` // 是否可显示
		TokenType    string `json:"tokenType"`    // 代币类型
		TokenLogo    string `json:"tokenLogo"`    // 代币 Logo
		TokenLevel   string `json:"tokenLevel"`   // 代币级别
		IssuerAddr   string `json:"issuerAddr"`   // 代币发行地址
		Vip          bool   `json:"vip"`          // 是否 VIP
	} `json:"tokenInfo"` // 代币信息
	ContractType          string `json:"contract_type"`         // 合约类型
	FromAddressIsContract bool   `json:"fromAddressIsContract"` // 发送者是否为合约
	ToAddressIsContract   bool   `json:"toAddressIsContract"`   // 接收者是否为合约
	RiskTransaction       bool   `json:"riskTransaction"`       // 是否为风险交易
}

// 定义 ApiResponse 结构体用于解析整个 API 响应
type ApiResponse struct {
	Total          int             `json:"total"`           // 总转账数量
	RangeTotal     int             `json:"rangeTotal"`      // 范围内的转账数量
	TokenTransfers []TokenTransfer `json:"token_transfers"` // 转账记录数组
}

// 定义一个结构体来存储转账信息
type TransferDetails struct {
	TokenAbbr     string
	TransactionID string
	Quant         float64
	FromAddress   string
	ToAddress     string
	FinalResult   string
}

// 传入钱包地址
func GetTransactions(order sdb.Orders) bool {

	/* 	// 获取当前时间戳（毫秒）
	   	endTime := carbon.Now().TimestampMilli()

	   	// 计算24小时前的时间戳（毫秒）
	   	startTime := carbon.Now().AddHours(-48).TimestampMilli() */

	// 构建请求的 URL 参数
	// API地址【trc20链的API地址】
	baseURL := "https://apilist.tronscan.org/api/token_trc20/transfers"
	params := url.Values{}
	// 要查询的钱包地址
	params.Add("toAddress", order.Token)
	params.Add("limit", "1") // 修改 limit 参数为 1，获取两条转账记录
	params.Add("confirm", "true")
	params.Add("start_timestamp", fmt.Sprintf("%d", order.StartTime))
	params.Add("end_timestamp", fmt.Sprintf("%d", order.ExpirationTime))
	// 增加合约地址
	params.Add("contract_address", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")

	// 使用 url 拼接完整的 URL
	finalURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	// 发起 GET 请求
	req, _ := http.NewRequest("GET", finalURL, nil)
	req.Header.Set("TRON-PRO-API-KEY", sdb.GetApiKey().Tronscan)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)

	if err != nil { // 如果请求失败，打印错误并退出
		// log.Fatalf("Error fetching data: %v", err)
		mylog.Logger.Error("USDT-TRC20 Error fetching data", zap.Any("error", err))
		return false
	}
	defer resp.Body.Close() // 确保请求结束后关闭响应体

	// 读取响应数据
	body, err := io.ReadAll(resp.Body)
	if err != nil { // 如果读取响应失败，打印错误并退出
		// log.Fatalf("Error reading response body: %v", err)
		mylog.Logger.Error("Error reading response body", zap.Any("error", err))
		return false
	}

	// 解析 JSON 响应到 ApiResponse 结构体
	var response ApiResponse
	err = json.Unmarshal(body, &response)
	if err != nil { // 如果 JSON 解析失败，打印错误并退出
		// log.Fatalf("Error unmarshalling JSON: %v", err)
		mylog.Logger.Error("Error unmarshalling JSON", zap.Any("error", err))
		return false
	}

	// 判断是否返回转账即可

	if len(response.TokenTransfers) > 0 {

		// 对返回的交易金额进行转换

		amount := formatAmount(response.TokenTransfers[0].Quant)

		if amount == order.ActualAmount && strings.EqualFold(response.TokenTransfers[0].ToAddress, order.Token) && response.TokenTransfers[0].TokenInfo.TokenAbbr == "USDT" && response.TokenTransfers[0].TransactionID != "" {
			// 如果满足条件，则说明已经查到转账记录，并且金额和数据库转换后的金额，则就可以更新数据库中
			// 原子入账：带状态守卫 + txHash 唯一去重，禁止裸 Save 全字段覆盖
			return sdb.MarkOrderPaid(order.TradeId, response.TokenTransfers[0].TransactionID)
		}
		mylog.Logger.Info("已经查询到转账记录，但是不符合要求")
		return false

	}
	mylog.Logger.Info("没有查询到转账记录")

	return false
}

// formatAmount 格式化金额为指定的小数位数，返回浮动数值（保留2位小数）
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
