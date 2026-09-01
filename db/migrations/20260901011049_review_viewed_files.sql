-- Create "review_viewed_files" table
CREATE TABLE "review_viewed_files" (
  "agent_id" uuid NOT NULL,
  "file_path" text NOT NULL,
  "fingerprint" text NOT NULL,
  "viewed_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("agent_id", "file_path"),
  CONSTRAINT "review_viewed_files_agent_id_fkey" FOREIGN KEY ("agent_id") REFERENCES "agents" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "review_viewed_files"
COMMENT ON TABLE "review_viewed_files" IS 'One file of an agent''s diff the person reviewing has read, and the shape that file had when they read it.';
-- Set comment to column: "file_path" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."file_path" IS 'The file that was read, named the way the diff names it: relative to the repository root, where the change left it if it moved it.';
-- Set comment to column: "fingerprint" on table: "review_viewed_files"
COMMENT ON COLUMN "review_viewed_files"."fingerprint" IS 'A hash of the file''s own slice of the patch as it stood when it was read. The panel hashes the file again on every load and shows it as unread once the two disagree, which is what makes an agent writing to the file undo the mark. It is the diff that is hashed rather than the file, since the hunks change when the base moves under a file nothing has touched.';
