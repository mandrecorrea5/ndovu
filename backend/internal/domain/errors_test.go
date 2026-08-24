package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidationError_ErrorFormatMostraContagem(t *testing.T) {
	err := NewValidationError("primeiro problema", "segundo problema", "terceiro")
	msg := err.Error()
	// A mensagem deve incluir a base "violação de contrato" + contagem.
	if !strings.Contains(msg, "violação de contrato") {
		t.Errorf("mensagem deveria conter 'violação de contrato', veio: %q", msg)
	}
	if !strings.Contains(msg, "3") {
		t.Errorf("mensagem deveria conter contagem '3', veio: %q", msg)
	}
}

func TestValidationError_ErrorsIsErrValidation(t *testing.T) {
	// ValidationError deve "envelopar" ErrValidation para que consumidores
	// possam fazer errors.Is(err, ErrValidation) sem se importar com os
	// issues específicos.
	err := NewValidationError("qualquer coisa")
	if !errors.Is(err, ErrValidation) {
		t.Error("errors.Is(err, ErrValidation) deveria ser true")
	}
	// Erros arbitrários NÃO devem casar.
	if errors.Is(err, ErrNotFound) {
		t.Error("errors.Is(validation, ErrNotFound) deveria ser false")
	}
}

func TestValidationError_ErrorsAsPermiteExtracao(t *testing.T) {
	// Consumidores devem conseguir extrair a lista de issues via errors.As.
	err := NewValidationError("issue-1", "issue-2")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatal("errors.As não conseguiu extrair *ValidationError")
	}
	if len(ve.Issues) != 2 {
		t.Errorf("esperava 2 issues, veio %d", len(ve.Issues))
	}
	if ve.Issues[0] != "issue-1" || ve.Issues[1] != "issue-2" {
		t.Errorf("issues fora de ordem: %v", ve.Issues)
	}
}

func TestValidationError_SemIssuesAindaConta(t *testing.T) {
	// Edge case: NewValidationError() sem argumentos ainda é um erro válido
	// (contagem 0). Útil como sentinela.
	err := NewValidationError()
	if err == nil {
		t.Fatal("NewValidationError() sem args não deve retornar nil")
	}
	if len(err.Issues) != 0 {
		t.Errorf("esperava 0 issues, veio %d", len(err.Issues))
	}
	if !errors.Is(err, ErrValidation) {
		t.Error("mesmo vazio, deve envelopar ErrValidation")
	}
}

func TestSentinelsNaoSaoIguais(t *testing.T) {
	// Sentinela existem para permitir switch/is discriminado — não devem
	// ser confundidos entre si.
	if errors.Is(ErrNotFound, ErrValidation) {
		t.Error("ErrNotFound e ErrValidation não devem ser iguais")
	}
	if ErrNotFound.Error() == ErrValidation.Error() {
		t.Error("mensagens de ErrNotFound e ErrValidation não devem ser iguais")
	}
}
