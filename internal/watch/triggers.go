package watch

import (
	"fmt"
	"strings"
)

// TriggerFunctions is every function a watch trigger runs.
var TriggerFunctions = []TriggerFunction{
	{
		Name: "watch_notify",
		Body: `
DECLARE
    ignored text[] := TG_ARGV[1:TG_NARGS - 1];
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM pg_notify(watch_channel_of(OLD.tenant_id), TG_TABLE_NAME || ' ' || (to_jsonb(OLD) ->> TG_ARGV[0]));
        RETURN NULL;
    END IF;
    IF TG_OP = 'UPDATE' AND to_jsonb(OLD) - ignored = to_jsonb(NEW) - ignored THEN
        RETURN NULL;
    END IF;
    PERFORM pg_notify(watch_channel_of(NEW.tenant_id), TG_TABLE_NAME || ' ' || (to_jsonb(NEW) ->> TG_ARGV[0]));
    RETURN NULL;
END;
`,
	},
	{
		Name: "watch_subresource",
		Body: `
BEGIN
    IF TG_OP <> 'INSERT' AND (to_jsonb(OLD) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify(watch_channel_of(OLD.tenant_id), TG_ARGV[0] || ' ' || (to_jsonb(OLD) ->> TG_ARGV[1]));
    END IF;
    IF TG_OP <> 'DELETE' AND (to_jsonb(NEW) ->> TG_ARGV[1]) IS NOT NULL THEN
        PERFORM pg_notify(watch_channel_of(NEW.tenant_id), TG_ARGV[0] || ' ' || (to_jsonb(NEW) ->> TG_ARGV[1]));
    END IF;
    RETURN NULL;
END;
`,
	},
	{
		Name: "watch_subresource_lookup",
		Body: `
DECLARE
    resource_key text;
    narrowing text := 'true';
    row_tenant uuid := (to_jsonb(COALESCE(NEW, OLD)) ->> 'tenant_id')::uuid;
BEGIN
    IF TG_OP = 'UPDATE' AND TG_ARGV[5] <> '' AND (to_jsonb(OLD) -> TG_ARGV[5]) IS NOT DISTINCT FROM (to_jsonb(NEW) -> TG_ARGV[5]) THEN
        RETURN NULL;
    END IF;
    IF TG_ARGV[4] <> '' THEN
        narrowing := FORMAT('%I IS NULL', TG_ARGV[4]);
    END IF;
    FOR resource_key IN EXECUTE FORMAT('SELECT %I::text FROM %I WHERE tenant_id = $2 AND %I = ($1).%I AND (%s)', TG_ARGV[1], TG_ARGV[0], TG_ARGV[2], TG_ARGV[3], narrowing)
        USING COALESCE(NEW, OLD), row_tenant LOOP
        PERFORM pg_notify(watch_channel_of(row_tenant), TG_ARGV[0] || ' ' || resource_key);
    END LOOP;
    RETURN NULL;
END;
`,
	},
}

// TriggerFunction is one function a watch trigger runs, with the plpgsql body a migration gives it.
type TriggerFunction struct {
	Name string
	Body string
}

// Trigger is one row trigger the watch needs attached for its resources to notify.
type Trigger struct {
	Name     string
	Table    string
	Function string
	Args     []string
}

// BuildTriggers lists every trigger the resources declare.
func BuildTriggers(resources []Resource) []Trigger {
	var triggers []Trigger
	for _, resource := range resources {
		triggers = append(triggers, Trigger{
			Name:     "watch_notify",
			Table:    resource.Table,
			Function: "watch_notify",
			Args:     append([]string{resource.KeyColumn}, resource.IgnoredColumns...),
		})

		for _, feeder := range resource.Feeders {
			trigger := Trigger{Name: "watch_subresource_" + resource.Table, Table: feeder.Table}
			if lookup := feeder.Lookup; lookup != nil {
				trigger.Function = "watch_subresource_lookup"
				trigger.Args = []string{resource.Table, resource.KeyColumn, lookup.ParentColumn, lookup.ChildColumn, lookup.SkipParentsWith, lookup.OnChangeOf}
			} else {
				trigger.Function = "watch_subresource"
				trigger.Args = []string{resource.Table, feeder.Column}
			}
			triggers = append(triggers, trigger)
		}
	}

	return triggers
}

// BuildSQL writes the statement that defines the function.
func (f TriggerFunction) BuildSQL() string {
	return fmt.Sprintf("CREATE OR REPLACE FUNCTION %s()\n    RETURNS TRIGGER\n    AS $$%s$$\nLANGUAGE plpgsql;\n", f.Name, f.Body)
}

// BuildSQL writes the statement that attaches the trigger.
func (t Trigger) BuildSQL() string {
	args := make([]string, len(t.Args))
	for i, arg := range t.Args {
		args[i] = quoteLiteral(arg)
	}

	return fmt.Sprintf("CREATE TRIGGER %s AFTER INSERT OR UPDATE OR DELETE ON %s FOR EACH ROW EXECUTE FUNCTION %s(%s);\n", t.Name, t.Table, t.Function, strings.Join(args, ", "))
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
