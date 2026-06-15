package USDT_ArbitrumOne

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

// APIResponse 表示API响应的整体结构
type APIResponse struct {
	Status  string        `json:"status"`
	Message string        `json:"message"`
	Result  []Transaction `json:"result"`
}

var Client = &http.Client{
	Timeout: time.Second * 30,
	// 设置代理
	/* 	Transport: &http.Transport{
		Proxy: http.ProxyURL(&url.URL{
			Scheme: "http",
			Host:   "127.0.0.1:7890",
		}),
	}, */
}

func GETHTTP(order sdb.Orders) (APIResponse, error) {

	var apiResponse APIResponse
	// 构建请求参数

	apiURL := "https://api.etherscan.io/v2/api"
	params := url.Values{}
	params.Add("chainid", "42161")
	params.Add("module", "account")
	params.Add("page", "1")
	params.Add("offset", "1")
	params.Add("sort", "desc")
	params.Add("apikey", sdb.GetApiKey().Etherscan)
	params.Add("action", "tokentx")
	params.Add("address", order.Token)
	params.Add("contractAddress", "0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9")

	URL := apiURL + "?" + params.Encode()

	// 请求的API地址：
	mylog.Logger.Info("请求的API地址:", zap.String("URL", URL))

	req, err := http.NewRequest("GET", URL, nil)
	if err != nil {
		return apiResponse, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := Client.Do(req)
	if err != nil {
		mylog.Logger.Error("Error fetching data", zap.Any("error", err))
		return apiResponse, err
	}
	defer resp.Body.Close()
	mylog.Logger.Info("Status Code:", zap.Int("statusCode", resp.StatusCode))
	// 读取返回结果
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiResponse, err
	}
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		return apiResponse, err
	}

	return apiResponse, nil
}

func Start(order sdb.Orders) bool {
	apiResponse, err := GETHTTP(order)
	if err != nil {
		mylog.Logger.Error("请求失败", zap.Error(err))
		return false
	}

	//

	if len(apiResponse.Result) > 0 {

		// 将时间字符串转为数字
		timeStamp, err := strconv.ParseInt(apiResponse.Result[0].TimeStamp, 10, 64)
		if err != nil {
			fmt.Println("时间戳转换失败:", err)
			return false
		}
		// 将时间转为毫秒
		timeStamp = timeStamp * 1000

		// 格式化金额数字
		amount := formatAmount(apiResponse.Result[0].Value)

		if timeStamp > order.StartTime && timeStamp < order.ExpirationTime && amount == order.ActualAmount && apiResponse.Result[0].Hash != "" && apiResponse.Result[0].TokenSymbol == "USD₮0" && strings.EqualFold(apiResponse.Result[0].To, order.Token) {
			// 原子入账：带状态守卫 + txHash 唯一去重，禁止裸 Save 全字段覆盖
			return sdb.MarkOrderPaid(order.TradeId, apiResponse.Result[0].Hash)

		}

	}

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
