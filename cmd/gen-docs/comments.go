package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

type comments map[protoreflect.FullName]string

func (c comments) of(descriptor protoreflect.Descriptor) string {
	return c[descriptor.FullName()]
}

// readComments builds the proto module under dir for its comments, which the generated Go descriptors do not carry.
func readComments(ctx context.Context, dir string) (comments, error) {
	command := exec.CommandContext(ctx, "buf", "build", dir, "--exclude-imports", "--output", "-")
	command.Stderr = os.Stderr

	image, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("building %s: %w", dir, err)
	}

	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(image, set); err != nil {
		return nil, fmt.Errorf("reading the image of %s: %w", dir, err)
	}

	files, err := protodesc.FileOptions{AllowUnresolvable: true}.NewFiles(set)
	if err != nil {
		return nil, fmt.Errorf("resolving the image of %s: %w", dir, err)
	}

	found := comments{}
	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
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

		return true
	})

	return found, nil
}

func recordMessages(messages protoreflect.MessageDescriptors, record func(protoreflect.Descriptor)) {
	for i := range messages.Len() {
		message := messages.Get(i)
		record(message)

		for f := range message.Fields().Len() {
			record(message.Fields().Get(f))
		}

		for o := range message.Oneofs().Len() {
			record(message.Oneofs().Get(o))
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
