// Command ygg-token emite um JWT HS256 assinado com JWT_SECRET, por exemplo o
// token de serviço do muninn (YGG_API_SERVICE_TOKEN):
//
//	JWT_SECRET=... go run ./cmd/ygg-token -sub muninn -ttl 720h
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/somoza00/yggdrasil/services/ygg-api/internal/middleware"
)

func main() {
	sub := flag.String("sub", "muninn", "subject (quem chama o ygg-api)")
	ttl := flag.Duration("ttl", 24*time.Hour, "validade do token")
	flag.Parse()

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("JWT_SECRET não definido")
	}
	tok, err := middleware.NewToken([]byte(secret), *sub, *ttl, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(tok)
}
