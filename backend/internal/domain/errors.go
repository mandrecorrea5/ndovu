package domain

import (
	"errors"
	"fmt"
)

// Erros de domínio. Os adapters HTTP os traduzem para status codes em um único lugar.
var (
	// ErrNotFound indica que o recurso consultado não existe.
	ErrNotFound = errors.New("recurso não encontrado")
	// ErrValidation indica violação do contrato de ingestão ou de consulta.
	ErrValidation = errors.New("violação de contrato")
)

// ValidationError acumula os problemas encontrados em um lote/consulta.
type ValidationError struct {
	Issues []string
}

func (v *ValidationError) Error() string {
	return fmt.Sprintf("%v: %d problema(s)", ErrValidation, len(v.Issues))
}

// Unwrap permite errors.Is(err, ErrValidation).
func (v *ValidationError) Unwrap() error { return ErrValidation }

// NewValidationError cria um erro de validação com os problemas informados.
func NewValidationError(issues ...string) *ValidationError {
	return &ValidationError{Issues: issues}
}
