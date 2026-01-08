package models

import (
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Tunnel struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	Name      string             `bson:"name"`
	CreatedAt time.Time          `bson:"created_at"`
}

type SerializableRequest struct {
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Header http.Header `json:"headers"`
	Body   string      `json:"body"`
	Host   string      `json:"host"`
}

type ResponseData struct {
	StatusCode int
	Headers    map[string][]string
	Body       string
}
