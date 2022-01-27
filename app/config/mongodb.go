package config

import (
	"context"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"log"
)

var mongoClient *mongo.Client
var mongodb *mongo.Database

func initMongodb() {
	var err error
	clientOptions := options.Client().ApplyURI(GetAppConfig().MongodbUri)
	// 连接到MongoDB
	mongoClient, err = mongo.Connect(context.TODO(), clientOptions)
	if err != nil {
		log.Fatal(err)
	}
	// 检查连接
	err = mongoClient.Ping(context.TODO(), nil)
	if err != nil {
		log.Fatal(err)
	}
	// 连接nginx
	mongodb = mongoClient.Database("nginx")
}

func GetMongoDb() *mongo.Database {
	if mongodb == nil {
		initMongodb()
	}
	return mongodb
}
