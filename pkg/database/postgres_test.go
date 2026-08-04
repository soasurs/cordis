package database

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

func TestNewPostgresRequiresDataSource(t *testing.T) {
	t.Parallel()

	_, err := NewPostgres(Config{})
	require.EqualError(t, err, "database data source is required")
}

func TestNewPostgresPoolRequiresDataSource(t *testing.T) {
	t.Parallel()

	_, err := NewPostgresPool(t.Context(), Config{})
	require.EqualError(t, err, "database data source is required")
}

func TestPostgresAttributesExcludeCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dataSource string
	}{
		{
			name:       "URL",
			dataSource: "postgres://cordis:url-secret@db.example.com:5433/cordis?sslmode=disable",
		},
		{
			name:       "keyword value",
			dataSource: "user=cordis password=keyword-secret host=db.example.com port=5433 dbname=cordis sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs, err := postgresAttributes(tt.dataSource)
			require.NoError(t, err)
			require.Contains(t, attrs, semconv.DBSystemNamePostgreSQL)
			require.Contains(t, attrs, semconv.ServerAddress("db.example.com"))
			require.Contains(t, attrs, semconv.ServerPort(5433))
			require.Contains(t, attrs, semconv.DBNamespace("cordis"))
			serialized := strings.ToLower(fmt.Sprint(attrs))
			require.NotContains(t, serialized, "url-secret")
			require.NotContains(t, serialized, "keyword-secret")
		})
	}
}
