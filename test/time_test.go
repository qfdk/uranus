package test

import (
	"fmt"
	"testing"
	"time"
)

func TestTime(t *testing.T) {
	dd, _ := time.ParseDuration("24h")
	dd1 := time.Now().Add(dd * 80)
	fmt.Println(dd1.Unix())
	fmt.Println(time.Unix(time.Now().Unix(), 0))
}
