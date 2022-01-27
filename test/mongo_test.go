package test

import (
	"context"
	"fmt"
	"github.com/qfdk/nginx-proxy-manager/app/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"testing"
)

func TestMongodb(t *testing.T) {
	fmt.Println("连接MongoDb")
	client := config.GetMgoCli()
	collections := client.Database("nginx").Collection("domains")

	//res, _ := collection.InsertOne(context.TODO(), bson.D{
	//	{"domains", []string{"yooo", "hhhhh"}},
	//	{"context", "contextcontenxt"}})
	//id := res.InsertedID.(primitive.ObjectID)
	//fmt.Println(id)
	//if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
	//	fmt.Println(oid)
	//	result, _ := collection.Find(context.TODO(), bson.M{"_id": primitive.ObjectIDFromHex("61f254ac2f3947c266e689e8")})
	//	fmt.Println(result)
	//}
	objID, _ := primitive.ObjectIDFromHex("61f254ac2f3947c266e689e8")
	var e bson.M
	collections.FindOne(context.TODO(), bson.M{"_id": objID}).Decode(&e)
	fmt.Println(e["_id"])
}
