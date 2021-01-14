package crd

import (
	//"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)
/*
func (a *GenericValidationAttrs) ApplyToSchema(schema *apiext.JSONSchemaProps) {
	// TODO: there must be at better way to do this
	// TODO: can we do type validation in some earlier pass?
	if a.Maximum != nil {
		val := float64(a.Maximum.Value)
		schema.Maximum = &val
	}
	if a.Minimum != nil {
		val := float64(a.Minimum.Value)
		schema.Minimum = &val
	}
	if a.ExclusiveMaximum {
		schema.ExclusiveMaximum = true
	}
	if a.ExclusiveMinimum {
		schema.ExclusiveMinimum = true
	}
	if a.MultipleOf != nil {
		val := float64(a.MultipleOf.Value)
		schema.MultipleOf = &val
	}



	if a.MaxLength != nil {
		val := a.MaxLength.Value
		schema.MaxLength = &val
	}
	if a.MinLength != nil {
		val := a.MinLength.Value
		schema.MinLength = &val
	}
	if a.Pattern != "" {
		schema.Pattern = a.Pattern
	}


	if a.MaxItems != nil {
		val := a.MaxItems.Value
		schema.MaxItems = &val
	}
	if a.MinItems != nil {
		val := a.MinItems.Value
		schema.MinItems = &val
	}
	if a.UniqueItems {
		schema.UniqueItems = a.UniqueItems
	}


	if a.Type != "" {
		schema.Type = a.Type
	}
	if a.Format != "" {
		schema.Format = a.Format
	}
}

// TODO: find a better way to do this (maybe have each attr be a message or something?)

func applyGenericValidation(ctx *schemaContext, attrs []*any.Any, schema *apiext.JSONSchemaProps) {
	// TODO: use DynamicAny and just look for ApplyToSchema
	var valid GenericValidationAttrs
	found := false
	for _, attrRaw := range attrs {
		if ptypes.Is(attrRaw, &valid) {
			err := ptypes.UnmarshalAny(attrRaw, &valid)
			if err != nil {
				ctx.AddError(attrRaw, fmt.Errorf("unable to unmarshal validation attributes: %w", err))
				return
			}
			found = true
			break
		}
	}
	if !found {
		return
	}

	valid.ApplyToSchema(schema)
}
*/
func applyGenericValidation(ctx *schemaContext, attrs []*any.Any, schema *apiext.JSONSchemaProps) {
}
