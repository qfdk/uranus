package test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
)

func TestHTTP(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.ip.sb/ip", nil)
	req.Header.Set("User-Agent", "Mozilla")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	body, _ := ioutil.ReadAll(resp.Body)
	fmt.Printf("%s\n", string(body))
}
