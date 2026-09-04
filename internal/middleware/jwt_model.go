package middleware

import (
	"github.com/golang-jwt/jwt/v5"
)

type UserLoad struct {
	UserDisplayName string `json:"userDisplayName,omitempty"`
	CardCode        string `json:"token"`
	Client          string `json:"client"`
	jwt.RegisteredClaims
}
