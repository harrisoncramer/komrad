-- Modify "marks" table
ALTER TABLE "marks" DROP CONSTRAINT "marks_pkey", ADD COLUMN "id" uuid NOT NULL DEFAULT uuidv7(), ADD PRIMARY KEY ("id");
-- Create index "marks_mark" to table: "marks"
CREATE UNIQUE INDEX "marks_mark" ON "marks" ("mark");
