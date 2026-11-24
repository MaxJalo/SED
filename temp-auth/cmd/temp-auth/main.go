package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/Maxjalo/SED/temp-auth/internal/httpapi"
	"github.com/Maxjalo/SED/temp-auth/internal/odata"
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
	if base := os.Getenv("ODATA_BASE_URL"); base != "" {
		timeoutSec, _ := strconv.Atoi(getEnv("ODATA_TIMEOUT_SEC", "60"))
		users, err := odata.LoadDirectory(context.Background(), odata.Config{
			BaseURL:  base,
			Username: os.Getenv("ODATA_USERNAME"),
			Password: os.Getenv("ODATA_PASSWORD"),
			OrgMatch: os.Getenv("ODATA_ORG_MATCH"),
			Timeout:  time.Duration(timeoutSec) * time.Second,
		})
		if err != nil {
			log.Fatalf("temp-auth odata directory: %v", err)
		}
		if len(users) == 0 {
			log.Fatal("temp-auth odata directory: пустой список сотрудников")
		}
		st = store.NewStore(users)
		log.Printf("temp-auth loaded %d employees from odata", len(users))
	}
	applyDemoLogins(st)
	srv := httpapi.NewServer(st, []byte(signingKey), time.Duration(ttlMin)*time.Minute)

	log.Printf("temp-auth listening on :%s (isolated dev auth service)", port)
	if err := http.ListenAndServe(":"+port, srv); err != nil {
		log.Fatalf("temp-auth failed: %v", err)
	}
}

func applyDemoLogins(st *store.Store) {
	adminLogin := os.Getenv("DEMO_ADMIN_LOGIN")
	adminPass := os.Getenv("DEMO_ADMIN_PASSWORD")
	managerLogin := os.Getenv("DEMO_MANAGER_LOGIN")
	managerPass := os.Getenv("DEMO_MANAGER_PASSWORD")
	if adminLogin == "" || adminPass == "" || managerLogin == "" || managerPass == "" {
		log.Printf("temp-auth demo logins are off")
		return
	}
	org := "ORG-01"
	counts := map[string]int{}
	best, bestN := "", 0
	for _, user := range st.ListDirectory("") {
		if user.OrganizationCode == "" {
			continue
		}
		counts[user.OrganizationCode]++
		if counts[user.OrganizationCode] > bestN {
			bestN = counts[user.OrganizationCode]
			best = user.OrganizationCode
		}
	}
	if best != "" {
		org = best
	}
	st.UpsertPasswordUser(store.User{
		UserID:           adminLogin,
		FullName:         "Администратор",
		Position:         "Администратор приложения",
		DepartmentCode:   "IT",
		DepartmentName:   "Администрирование",
		OrganizationCode: org,
		Roles:            []string{"admin"},
	}, adminPass)
	st.UpsertPasswordUser(store.User{
		UserID:           managerLogin,
		FullName:         "Менеджер маршрутов",
		Position:         "Владелец процесса",
		DepartmentCode:   "IT",
		DepartmentName:   "Администрирование",
		OrganizationCode: org,
		Roles:            []string{"manager"},
	}, managerPass)
	log.Printf("temp-auth password logins enabled")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
