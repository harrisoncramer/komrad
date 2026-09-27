-- Modify "document_versions" table
ALTER TABLE "document_versions" ADD CONSTRAINT "document_versions_body_written" CHECK (body <> ''::text);
-- Modify "documents" table
ALTER TABLE "documents" ADD CONSTRAINT "documents_body_written" CHECK (body <> ''::text);
