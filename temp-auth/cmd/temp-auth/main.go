package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Maxjalo/SED/temp-auth/internal/httpapi"
	"github.com/Maxjalo/SED/temp-auth/internal/store"
)

func main() {
	port := getEnv("TEMP_AUTH_PORT", "8091")
	signingKey := getEnv("TEMP_AUTH_SIGNING_KEY", "dev_ephemeral_jwt_signing_key_change_in_prod")
	ttlMin, err := strconv.Atoi(getEnv("TEMP_AUTH_TOKEN_TTL_MIN", "60"))
	if err != nil || ttlMin <= 0 {
		ttlMin = 60
	}

	st := store.NewDefaultStore()
	srv := httpapi.NewServer(st, []byte(signingKey), time.Duration(ttlMin)*time.Minute)

	log.Printf("temp-auth listening on :%s (isolated dev auth service)", port)
	if err := http.ListenAndServe(":"+port, srv); err != nil {
		log.Fatalf("temp-auth failed: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
