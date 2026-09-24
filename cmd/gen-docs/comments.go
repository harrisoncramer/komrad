package main

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type comments map[protoreflect.FullName]string

func (c comments) of(descriptor protoreflect.Descriptor) string {
	return c[descriptor.FullName()]
}

// readComments compiles the proto sources for their comments, which the generated Go descriptors
// do not carry.
func readComments(ctx context.Context, protoDir string) (comments, error) {
	var paths []string
	err := filepath.WalkDir(protoDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".proto" {
			return err
		}

		relative, err := filepath.Rel(protoDir, path)
		if err != nil {
			return err
		}

		paths = append(paths, filepath.ToSlash(relative))

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", protoDir, err)
	}

	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(protocompile.CompositeResolver{
			&protocompile.SourceResolver{ImportPaths: []string{protoDir}},
			protocompile.ResolverFunc(func(path string) (protocompile.SearchResult, error) {
				file, err := protoregistry.GlobalFiles.FindFileByPath(path)
				if err != nil {
					return protocompile.SearchResult{}, err
				}

				return protocompile.SearchResult{Desc: file}, nil
			}),
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}

	files, err := compiler.Compile(ctx, paths...)
	if err != nil {
		return nil, fmt.Errorf("compiling %s: %w", protoDir, err)
	}

	found := comments{}
	for _, file := range files {
		locations := file.SourceLocations()
		record := func(descriptor protoreflect.Descriptor) {
			if text := cleanComment(locations.ByDescriptor(descriptor).LeadingComments); text != "" {
				found[descriptor.FullName()] = text
			}
		}

		for i := range file.Services().Len() {
			service := file.Services().Get(i)
			record(service)

			for m := range service.Methods().Len() {
				record(service.Methods().Get(m))
			}
		}

		recordEnums(file.Enums(), record)
		recordMessages(file.Messages(), record)
	}

	return found, nil
}

func recordMessages(messages protoreflect.MessageDescriptors, record func(protoreflect.Descriptor)) {
	for i := range messages.Len() {
		message := messages.Get(i)
		record(message)

		for f := range message.Fields().Len() {
			record(message.Fields().Get(f))
		}

		recordEnums(message.Enums(), record)
		recordMessages(message.Messages(), record)
	}
}

func recordEnums(enums protoreflect.EnumDescriptors, record func(protoreflect.Descriptor)) {
	for i := range enums.Len() {
		enum := enums.Get(i)
		record(enum)

		for v := range enum.Values().Len() {
			record(enum.Values().Get(v))
		}
	}
}

// cleanComment joins a comment's wrapped lines into paragraphs.
func cleanComment(raw string) string {
	var paragraphs []string
	for _, block := range strings.Split(strings.TrimSpace(raw), "\n\n") {
		if joined := strings.Join(strings.Fields(block), " "); joined != "" {
			paragraphs = append(paragraphs, joined)
		}
	}

	return strings.Join(paragraphs, "\n\n")
}
