package sdb

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"time"
	"upay_pro/mylog"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"golang.org/x/crypto/bcrypt"
)

var DB *gorm.DB

func init() {
	// 确保目录存在
	// 创建目录
	os.MkdirAll("DBS", 0755)
	db, err := gorm.Open(sqlite.Open("DBS/upay_pro.db"), &gorm.Config{})
	if err != nil {
		mylog.Logger.Error("open db error", zap.Error(err))
		mylog.Logger.Sync()
	}
	mylog.Logger.Info("数据库链接成功")
	DB = db
	Start()
}

type User struct {
	gorm.Model
	UserName string `gorm:"column:UserName"`
	PassWord string `gorm:"column:PassWord"`
}

// 订单状态
const (
	StatusWaitPay     = 1 // 等待支付
	StatusPaySuccess  = 2 // 支付成功
	StatusExpired     = 3 // 已过期
	CallBackConfirmOk = 1 // 回调已确认
	CallBackConfirmNo = 2 // 回调未确认
)

// 订单表
type Orders struct {
	gorm.Model
	TradeId            string  // UPAY订单号
	OrderId            string  // 客户交易id
	BlockTransactionId string  // 区块id
	Amount             float64 // 订单金额，保留2位小数
	ActualAmount       float64 // 订单实际需要支付的金额，保留2位小数（按分区分订单，实现处统一 math.Round(x*100)/100）
	Type               string  //钱包类型
	Token              string  // 所属钱包地址
	Status             int     // 1：等待支付，2：支付成功，3：已过期

	NotifyUrl       string // 异步回调地址
	RedirectUrl     string // 同步回调地址
	CallbackNum     int    // 回调次数
	CallBackConfirm int    // 回调是否已确认 1是 2否
	StartTime       int64  // 订单开始时间（时间戳）
	ExpirationTime  int64  // 订单过期时间（时间戳）

}

// 钱包状态
const (
	TokenStatusEnable  = 1 // 钱包启用
	TokenStatusDisable = 2 // 钱包禁用
)

// 钱包地址表
type WalletAddress struct {
	gorm.Model
	Currency string  // 币种
	Token    string  // 钱包token
	Status   int     // 1:启用 2:禁用
	Rate     float64 // 汇率
	AutoRate bool    `gorm:"column:AutoRate;default:false"` // 汇率是否自动维护

	// - 0 ：表示 false ，即 禁用 自动汇率功能
	// - 1 ：表示 true ，即 启用 自动汇率功能
}

// 汇率维护表
/* type AutoRate struct {
	gorm.Model
	Currency string `gorm:"column:currency"`                // 币种
	AutoRate bool   `gorm:"column:autoRate default:false" ` // 汇率是否自动维护
} */

type Setting struct {
	gorm.Model
	AppUrl                 string
	SecretKey              string
	Httpport               int
	Tgbotkey               string
	Tgchatid               string
	Barkkey                string
	Redishost              string
	Redisport              int
	Redispasswd            string
	Redisdb                int
	ExpirationDate         time.Duration
	AppName                string //应用名称
	CustomerServiceContact string //客户服务联系方式

}
type ApiKey struct {
	gorm.Model
	Tronscan  string
	Trongrid  string
	Etherscan string
}

// 创建一个单独的表用来存储订单号和队列ID

type TradeIdTaskID struct {
	gorm.Model
	// 这里的订单号是系统订单号不是商户订单号
	TradeId string `gorm:"column:TradeId"`
	TaskID  string `gorm:"column:TaskID"`
}

func Start() {
	mylog.Logger.Info("开始初始化数据库")
	mylog.Logger.Info("开始迁移数据库")
	// 迁移用户表
	DB.AutoMigrate(&User{})

	// 初始化用户表
	result := DB.First(&User{})
	if result.Error != nil {
		mylog.Logger.Info("获取用户表失败")
	}
	if result.RowsAffected == 0 {
		mylog.Logger.Info("用户表为空")

		hashedPassword, _ := HashPassword(Defaultuserpassword)
		mylog.Logger.Info("初始用户名:", zap.String("username", defaultuserusername))
		mylog.Logger.Info("初始密码:", zap.String("password", Defaultuserpassword))
		// 创建用户
		result := DB.Create(&User{
			UserName: defaultuserusername,
			PassWord: hashedPassword,
		})
		if result.Error != nil {
			mylog.Logger.Info("创建用户失败")
		} else {
			mylog.Logger.Info("创建用户成功")
		}

	}

	// 迁移订单表
	DB.AutoMigrate(&Orders{})

	// 为区块交易ID(block_transaction_id)建立「部分唯一索引」：仅对非空值去重。
	// 作用：同一笔链上交易(txHash)只能入账一个订单，作为重复入账的最后防线。
	// 用部分索引(WHERE ...<>'')是因为大量待支付订单的该字段为空串，普通唯一索引会因多个空串而冲突。
	if err := DB.Exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_block_tx_id ON orders(block_transaction_id) WHERE block_transaction_id <> '' AND block_transaction_id IS NOT NULL",
	).Error; err != nil {
		mylog.Logger.Error("创建 block_transaction_id 唯一索引失败", zap.Error(err))
	}
	// 迁移钱包地址表
	DB.AutoMigrate(&WalletAddress{})
	// 迁移设置表
	DB.AutoMigrate(&Setting{})
	//迁移apikey表
	DB.AutoMigrate(&ApiKey{})
	// 检查设置表是否为空，如果为空则插入默认设置
	var settingCount int64
	DB.Model(&Setting{}).Count(&settingCount)
	// 给设置表设置默认值
	if settingCount == 0 {
		mylog.Logger.Info("设置表为空，创建默认设置")
		result := DB.Create(&Setting{
			AppUrl:                 "http://localhost",
			SecretKey:              GenerateSecretKey(48),
			Httpport:               8090,
			Tgbotkey:               "",
			Tgchatid:               "",
			Barkkey:                "",
			Redishost:              "127.0.0.1",
			Redisport:              6379,
			Redispasswd:            "",
			Redisdb:                0,
			ExpirationDate:         ExpirationDate,
			AppName:                "",
			CustomerServiceContact: "",
		})
		if result.Error != nil {
			mylog.Logger.Error("创建默认设置失败", zap.Error(result.Error))
		} else {
			mylog.Logger.Info("默认设置创建成功")
		}
	}

	// 给APIKEY表设置默认值
	var apikeyCount int64
	// 检查APIKEY表是否为空，如果为空则插入默认值
	DB.Model(&ApiKey{}).Count(&apikeyCount)
	if apikeyCount == 0 {
		mylog.Logger.Info("APIKEY表为空，创建默认设置")
		result := DB.Create(&ApiKey{
			Tronscan:  "28b6e96a-4630-442e-8f2b-35f80c8b54d6",
			Trongrid:  "0232af66-3f6f-42a3-bd90-f184b38fba27",
			Etherscan: "UPCN5AHEA1383NW5DUYZ3REE8V38TSS94N",
		})
		if result.Error != nil {
			mylog.Logger.Error("APIKEY表创建默认设置失败", zap.Error(result.Error))
		} else {
			mylog.Logger.Info("APIKEY表默认设置创建成功")
		}
	}
	// 迁移订单号和队列ID表
	DB.AutoMigrate(&TradeIdTaskID{})
	// 迁移汇率维护表
	// DB.AutoMigrate(&AutoRate{})

}

const (
	ExpirationDate = time.Minute * 10
)

var (
	defaultuserusername = GenerateSecretKey(8)
	Defaultuserpassword = GenerateSecretKey(8)
)

// 设置一个生成密钥的函数

func GenerateSecretKey(length int) string {

	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var key bytes.Buffer
	for i := 0; i < length; i++ {
		key.WriteByte(chars[rand.Intn(len(chars))])
	}
	return key.String()
}

func GetSetting() Setting {
	var setting Setting
	DB.First(&setting)

	/* if result.RowsAffected == 0 {
		mylog.Logger.Info("系统设置不存在，创建默认设置")
		// 创建默认设置
		defaultSetting := Setting{
			AppUrl:                 "",
			SecretKey:              GenerateSecretKey(48),
			Httpport:               8080,
			Tgbotkey:               "",
			Tgchatid:               "",
			Barkkey:                "",
			Redishost:              "127.0.0.1",
			Redisport:              6379,
			Redispasswd:            "",
			Redisdb:                0,
			ExpirationDate:         ExpirationDate,
			AppName:                "",
			CustomerServiceContact: "",
		}
		createResult := DB.Create(&defaultSetting)
		if createResult.Error != nil {
			mylog.Logger.Error("创建默认设置失败", zap.Error(createResult.Error))
			return setting // 返回空设置
		}
		return defaultSetting
	} */

	return setting
}

func HashPassword(password string) (string, error) {
	cost := 12 // 计算成本，值越大越安全但越耗时
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hashedBytes), nil
}

// VerifyPassword 验证输入密码是否匹配存储的哈希
func VerifyPassword(inputPassword, storedHash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(inputPassword))
	return err == nil // true 表示验证通过
}

// 因为同样的钱包类型，可能有多个钱包地址，所以这里返回一个数组
func GetWalletAddress(type_ string) []WalletAddress {

	var walletAddress []WalletAddress

	DB.Where("currency = ? and status = ?", type_, TokenStatusEnable).Find(&walletAddress)
	return walletAddress
}

func (n WalletAddress) String() string {
	return fmt.Sprintf("%s:%v", n.Token, n.Rate)
}

func GetOrderByOrderId(orderId string) Orders {
	var order Orders
	DB.Where("order_id = ?", orderId).Last(&order)
	return order
}

func GetApiKey() ApiKey {
	var apikey ApiKey
	DB.First(&apikey)
	return apikey
}

func GetUserByUsername() string {
	var user User
	re := DB.First(&user)
	if re.Error != nil {
		mylog.Logger.Error("查询用户失败", zap.Error(re.Error))
		return ""
	}
	return user.UserName
}

// MarkOrderPaid 统一的原子入账函数：把订单标记为支付成功。
//
// 资产安全要点（所有入账路径——各币种扫块/API、手动补单——都必须走本函数，禁止再用 DB.Save(&order) 裸覆盖）：
//  1. 状态守卫：带 WHERE status = StatusWaitPay 条件更新，已过期/已支付的订单不会被改动，
//     杜绝「已过期订单被扫块复活」「并发重复入账」。
//  2. txHash 去重：blockTransactionId 配合数据库唯一索引（见 Start() 中的条件唯一索引），
//     同一笔链上交易只能入账一个订单；若该 txHash 已被占用，UPDATE 触发唯一约束冲突返回 error。
//  3. 只更新受影响列（status、block_transaction_id），不全字段覆盖，避免覆盖回调状态等并发写入的字段。
//
// 返回 true 表示本次确实由「待支付」改为「支付成功」（RowsAffected==1）；
// 返回 false 表示订单已非待支付（被其他路径抢先入账/已过期）或写入失败，调用方不应再触发回调。
func MarkOrderPaid(tradeId string, blockTransactionId string) bool {
	result := DB.Model(&Orders{}).
		Where("trade_id = ? AND status = ?", tradeId, StatusWaitPay).
		Updates(map[string]interface{}{
			"status":               StatusPaySuccess,
			"block_transaction_id": blockTransactionId,
		})
	if result.Error != nil {
		// 唯一索引冲突（该 txHash 已入账其他订单）或数据库错误都会走到这里
		mylog.Logger.Error("订单入账失败", zap.String("trade_id", tradeId),
			zap.String("block_transaction_id", blockTransactionId), zap.Error(result.Error))
		return false
	}
	if result.RowsAffected == 0 {
		// 0 行：订单已非待支付（已被抢先入账或已过期），属正常的并发/幂等结果
		mylog.Logger.Info("订单非待支付状态，跳过入账", zap.String("trade_id", tradeId))
		return false
	}
	mylog.Logger.Info("订单入账成功", zap.String("trade_id", tradeId),
		zap.String("block_transaction_id", blockTransactionId))
	return true
}
