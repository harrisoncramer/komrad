export type Field = {
  name: string;
  type: string;
  ref: string;
  repeated: boolean;
  map: boolean;
  oneof: string;
  rules: string[];
  description: string;
};

export type Message = {
  name: string;
  description: string;
  fields: Field[];
};

export type Enum = {
  name: string;
  description: string;
  values: { name: string; description: string }[];
};

export type Method = {
  slug: string;
  name: string;
  path: string;
  command: string;
  tool: string;
  mcp: boolean;
  roles: string[];
  summary: string;
  description: string;
  readOnly: boolean;
  idempotent: boolean;
  destructive: boolean;
  request: string;
  response: string;
};

export type Service = {
  slug: string;
  title: string;
  name: string;
  description: string;
  methods: Method[];
};

export type API = {
  services: Service[];
  messages: Record<string, Message>;
  enums: Record<string, Enum>;
};

export type Flag = {
  name: string;
  shorthand: string;
  type: string;
  default: string;
  usage: string;
};

export type Command = {
  slug: string;
  path: string;
  parent: string;
  usage: string;
  aliases: string[];
  short: string;
  long: string;
  example: string;
  flags: Flag[];
  inherited: Flag[];
  children: string[];
};

export type Setting = {
  key: string;
  path: string;
  type: string;
  description: string;
  default: string;
};

export type Section = {
  path: string;
  description: string;
  settings: Setting[];
  sections: Section[];
};
