-- Create index "documents_body_trgm" to table: "documents"
CREATE INDEX "documents_body_trgm" ON "documents" USING GIN ("body" gin_trgm_ops);
