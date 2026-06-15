package mq

import (
	"context"
	"fmt"
	"time"
	"upay_pro/db/sdb"
	"upay_pro/mylog"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// 客户端
var Client *asynq.Client

// 服务端
var Mux *asynq.ServeMux

// 任务管理器
var Inspector *asynq.Inspector

func init() {
	// 获取redis地址
	addr := fmt.Sprintf("%s:%d", sdb.GetSetting().Redishost, sdb.GetSetting().Redisport)
	// 初始客户端
	client := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     addr,
		Password: sdb.GetSetting().Redispasswd,
		DB:       sdb.GetSetting().Redisdb,
	})
	Client = client
	// 初始化任务管理器
	Inspector = asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     addr,
		Password: sdb.GetSetting().Redispasswd,
		DB:       sdb.GetSetting().Redisdb,
	})
	// 启动异步任务服务器
	go async_server_run()

}

// QueueOrderExpiration 订单过期任务的队列名称
const QueueOrderExpiration = "order:expiration"

// TaskOrderExpiration 创建任务和任务加入对列
func TaskOrderExpiration(payload string, expirationDuration time.Duration) {
	task := asynq.NewTask(QueueOrderExpiration, []byte(payload)) // 转换为字节切片
	// 将任务加入队列
	info, err := Client.Enqueue(task, asynq.ProcessIn(expirationDuration))
	if err != nil {
		mylog.Logger.Info("任务加入失败:" + err.Error())
	}
	mylog.Logger.Info("任务已加入队列:", zap.Any("info", info))

	// 把订单号和任务ID存在数据库中，方便使用
	var tradeIdTaskID sdb.TradeIdTaskID
	tradeIdTaskID.TradeId = payload
	tradeIdTaskID.TaskID = info.ID
	// 不存在就创建，存在就更新现有的记录
	sdb.DB.Create(&tradeIdTaskID)

}

// 队列服务端
func async_server_run() {
	Mux = asynq.NewServeMux()
	// 注册处理函数，根据任务名称，调用不同的处理函数
	Mux.HandleFunc(QueueOrderExpiration, handleCheckStatusCodeTask)
	// 获取redis地址
	addr := fmt.Sprintf("%s:%d", sdb.GetSetting().Redishost, sdb.GetSetting().Redisport)
	server := asynq.NewServer(asynq.RedisClientOpt{
		Addr:     addr,
		Password: sdb.GetSetting().Redispasswd,
		DB:       sdb.GetSetting().Redisdb,
	}, asynq.Config{Concurrency: 10})
	if err := server.Run(Mux); err != nil {
		mylog.Logger.Info("Error starting server:", zap.Any("err", err))
	}
}

// 处理过期任务
func handleCheckStatusCodeTask(ctx context.Context, t *asynq.Task) error {

	// 提取任务载荷传入的交易ID，根据ID去查一下订单记录里面的支付状态是否是待支付，如果是待支付，改为已过期
	// 订单过期后，需要解锁钱包地址和金额【从Redis里删除】
	payload := string(t.Payload())

	// 用带条件的原子 UPDATE 把订单改为过期：仅当当前仍为「等待支付」时才生效。
	// 不能先 First 读出再 Save 全字段覆盖——读和写之间若扫块协程刚把订单改为支付成功，
	// 旧快照的 Save 会把「支付成功」覆盖回「已过期」，造成已付款订单被误判过期（资产风险）。
	// 把状态判定下推到 SQL 的 WHERE，由数据库保证原子性。
	result := sdb.DB.Model(&sdb.Orders{}).
		Where("trade_id = ? AND status = ?", payload, sdb.StatusWaitPay).
		Update("status", sdb.StatusExpired)
	if result.Error != nil {
		mylog.Logger.Error("订单过期状态更新失败", zap.String("trade_id", payload), zap.Error(result.Error))
		return result.Error
	}
	if result.RowsAffected > 0 {
		mylog.Logger.Info(fmt.Sprintf("订单%v已设置为过期", payload))
	} else {
		// 0 行受影响：订单已支付成功或已是其他状态，无需过期处理
		mylog.Logger.Info("订单非等待支付状态，跳过过期处理", zap.String("trade_id", payload))
	}

	// 根据订单号查到记录，删除记录
	var task sdb.TradeIdTaskID

	re := sdb.DB.Where("TradeId = ?", payload).Delete(&task)
	if re.Error != nil {
		mylog.Logger.Info("删除数据库TradeIdTaskID中的任务记录失败", zap.Error(re.Error))
		return re.Error
	}

	return nil
}

// 终止任务
func StopTask(taskID string) error {
	// 从队列中删除任务
	err := Inspector.DeleteTask("default", taskID)
	if err != nil {
		mylog.Logger.Info("删除任务失败")
		return err
	}
	return nil
}
