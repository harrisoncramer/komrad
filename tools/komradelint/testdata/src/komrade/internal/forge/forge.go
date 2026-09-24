package forge

type Secret struct{ value string }

func NewSecret(value string) Secret { return Secret{value: value} }

func (s Secret) Expose() string { return s.value }

type Credential struct {
	Header string
	Token  Secret
}

func (c Credential) HeaderValue() string {
	return "Bearer " + c.Token.Expose()
}

type FileStore struct{}

func (f *FileStore) Set(id string, secret Secret) error {
	_ = secret.Expose()

	return nil
}

func (f *FileStore) Get(id string) (Secret, error) {
	return Secret{}, nil
}

func leak(s Secret) string {
	return s.Expose() // want `Expose gives up the token; komrade/internal/forge.leak is not a place allowed to send or keep it`
}
