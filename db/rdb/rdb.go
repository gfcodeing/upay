package rdb

import (
	"context"
	"fmt"
	"os"
	"time"
	"upay_pro/db/sdb"
	"upay_pro/mylog"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var RDB *redis.Client

func init() {
	setting := sdb.GetSetting()

	// Redis 地址：优先读环境变量 REDIS_HOST/REDIS_PORT，没有则读数据库配置
	redisHost := setting.Redishost
	if envHost := os.Getenv("REDIS_HOST"); envHost != "" {
		redisHost = envHost
	}
	redisPort := setting.Redisport
	redisPasswd := setting.Redispasswd
	// Redis 密码：优先读环境变量 REDIS_PASS，没有则读数据库配置
	if envPass := os.Getenv("REDIS_PASS"); envPass != "" {
		redisPasswd = envPass
	}

	// 创建 Redis 客户端
	rdb := redis.NewClient(&redis.Options{
		// 基本连接配置
		Addr:     fmt.Sprintf("%s:%d", redisHost, redisPort), // Redis 地址
		Password: redisPasswd,                                // Redis 密码
		DB:       setting.Redisdb,                            // 数据库编号

		// 连接超时设置
		DialTimeout:  10 * time.Second, // 建立连接超时时间
		ReadTimeout:  30 * time.Second, // 读取超时时间
		WriteTimeout: 30 * time.Second, // 写入超时时间

		// 连接池设置
		PoolSize:        10,               // 连接池最大连接数
		MinIdleConns:    5,                // 最小空闲连接数
		PoolTimeout:     4 * time.Second,  // 从连接池获取连接的超时时间
		ConnMaxLifetime: 30 * time.Minute, // 连接的最大存活时间（替代 MaxConnAge）
		ConnMaxIdleTime: 5 * time.Minute,  // 空闲连接超时时间（替代 IdleTimeout）

		// 其他设置
		OnConnect: func(ctx context.Context, cn *redis.Conn) error {
			// 连接建立时的回调函数
			return nil
		},
	})
	ctx := context.Background()
	RDB = rdb
	// defer rdb.Close()  在其他调用时最后关闭
	// 测试连接
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		// redis 连接失败写入日志
		mylog.Logger.Panic("redis 链接失败", zap.Error(err))

	}

	// 测试redis是否连接成功 写入日志
	mylog.Logger.Info("redis 连接成功")

	/* 	// 测试访问不存在的键
	   	_, err = rdb.Get(ctx, "520").Result()
	   	if err != nil {
	   		log.Logger.Info("redis 访问不存在的键")
	   	} */

}

// Close 优雅关闭 Redis 连接
