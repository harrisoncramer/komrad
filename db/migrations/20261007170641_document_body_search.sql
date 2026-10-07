-- Create index "documents_body_trgm" to table: "documents"
CREATE INDEX "documents_body_trgm" ON "documents" USING GIN ("body" gin_trgm_ops);
-- Create index "documents_body_tsv" to table: "documents"
CREATE INDEX "documents_body_tsv" ON "documents" USING GIN ((to_tsvector('english_nostop'::regconfig, body)));
