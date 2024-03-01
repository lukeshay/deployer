package image

import "fmt"

type Image struct {
	name      string
	uniqueTag string
	tags      []string
}

type ImageOptions struct {
	Name      string
	UniqueTag string
	Tags      []string
}

func NewImage(opts ImageOptions) Image {
	return Image{
		name:      opts.Name,
		uniqueTag: opts.UniqueTag,
		tags:      opts.Tags,
	}
}

func (i Image) Name() string {
	return FormatImageName(i.name, i.uniqueTag)
}

func (i Image) Names() []string {
	names := make([]string, len(i.tags))

	for idx, tag := range i.tags {
		names[idx] = FormatImageName(i.name, tag)
	}

	return names
}

func FormatImageName(name, tag string) string {
	return fmt.Sprintf("%s:%s", name, tag)
}
