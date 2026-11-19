// sign-release 给发布清单签名。scripts/release.sh 用它补全并签名 dist/version.json，
// 节点升级前用编在程序里的公钥（internal/services/update.go 的 releasePublicKey）验证。
//
//	go run ./cmd/sign-release -genkey <私钥文件>        生成密钥，打印公钥
//	go run ./cmd/sign-release -key <私钥文件> <version.json>
//	                                                     写入同目录各程序的 sha256 和大小，再写出 version.json.sig
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	genkey := flag.String("genkey", "", "生成新的签名密钥并写到这个文件")
	keyFile := flag.String("key", "", "签名用的私钥文件")
	flag.Parse()

	if *genkey != "" {
		if err := generate(*genkey); err != nil {
			fail(err)
		}
		return
	}
	if *keyFile == "" || flag.NArg() != 1 {
		fail(fmt.Errorf("用法：sign-release -key <私钥文件> <version.json>"))
	}
	if err := sign(*keyFile, flag.Arg(0)); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func generate(file string) error {
	if _, err := os.Stat(file); err == nil {
		return fmt.Errorf("%s 已存在，不覆盖：换密钥后，已经部署的节点都不再认新发布的版本", file)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(file, []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func sign(keyFile, manifestFile string) error {
	keyText, err := os.ReadFile(keyFile)
	if err != nil {
		return err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyText)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("%s 不是签名私钥", keyFile)
	}
	key := ed25519.NewKeyFromSeed(seed)

	data, err := os.ReadFile(manifestFile)
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("%s: %v", manifestFile, err)
	}
	files := map[string]any{}
	dir := filepath.Dir(manifestFile)
	for _, name := range []string{"uranus-amd64", "uranus-arm64"} {
		sum, size, err := hashFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		files[name] = map[string]any{"sha256": sum, "size": size}
	}
	manifest["files"] = files
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(manifestFile, data, 0o644); err != nil {
		return err
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(key, data))
	return os.WriteFile(manifestFile+".sig", []byte(sig+"\n"), 0o644)
}

func hashFile(file string) (string, int64, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), size, err
}
