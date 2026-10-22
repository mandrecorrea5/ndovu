-- Apps de servidor podem enviar OTLP (docs/AVALIACAO-OTLP.md §3.2). O flag é
-- do app, não da chave: cada app tem uma chave ativa e a rotação cria outra,
-- então um flag na chave se perderia a cada rotação. Default false: chave de
-- browser é pública e não deve aceitar spans.
ALTER TABLE apps
    ADD COLUMN allow_otlp boolean NOT NULL DEFAULT false;
