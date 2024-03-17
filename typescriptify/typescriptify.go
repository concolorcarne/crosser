// A pared down version of github.com/tkrajina/typescriptify-golang-structs

package typescriptify

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/tkrajina/go-reflector/reflector"
)

const (
	tsDocTag            = "ts_doc"
	tsTransformTag      = "ts_transform"
	tsType              = "ts_type"
	jsonTag             = "json"
	validateTagName     = "validate"
	tsConvertValuesFunc = `convertValues(a: any, classs: any, asMap: boolean = false): any {
	if (!a) {
		return a;
	}
	if (a.slice) {
		return (a as any[]).map(elem => this.convertValues(elem, classs));
	} else if ("object" === typeof a) {
		if (asMap) {
			for (const key of Object.keys(a)) {
				a[key] = new classs(a[key]);
			}
			return a;
		}
		return new classs(a);
	}
	return a;
}`
)

// TypeOptions overrides options set by `ts_*` tags.
type TypeOptions struct {
	TSType      string
	TSDoc       string
	TSTransform string
}

// StructType stores settings for transforming one Golang struct.
type StructType struct {
	Type         reflect.Type
	FieldOptions map[reflect.Type]TypeOptions
}

type EnumType struct {
	Type reflect.Type
}

type FunctionParameter struct {
	Name string
	Type string
}

type TypeScriptFunction struct {
	IsAsync    bool
	Name       string
	Parameters []FunctionParameter
	ReturnType string
	Body       []string
	DontExport bool
}

type enumElement struct {
	value interface{}
	name  string
}

type TypeScriptify struct {
	Prefix            string
	Suffix            string
	Indent            string
	CreateConstructor bool
	BackupDir         string // If empty no backup
	DontExport        bool
	CreateInterface   bool
	CustomJsonTag     string
	Quiet             bool // surpress logs when building output
	customImports     []string

	structTypes []StructType
	enumTypes   []EnumType
	enums       map[reflect.Type][]enumElement
	kinds       map[reflect.Kind]string
	functions   []TypeScriptFunction

	fieldTypeOptions map[reflect.Type]TypeOptions

	// throwaway, used when converting
	alreadyConverted map[reflect.Type]bool
}

func New() *TypeScriptify {
	result := new(TypeScriptify)
	result.Indent = "\t"
	result.BackupDir = "."

	kinds := make(map[reflect.Kind]string)

	kinds[reflect.Bool] = "boolean"
	kinds[reflect.Interface] = "any"

	kinds[reflect.Int] = "number"
	kinds[reflect.Int8] = "number"
	kinds[reflect.Int16] = "number"
	kinds[reflect.Int32] = "number"
	kinds[reflect.Int64] = "number"
	kinds[reflect.Uint] = "number"
	kinds[reflect.Uint8] = "number"
	kinds[reflect.Uint16] = "number"
	kinds[reflect.Uint32] = "number"
	kinds[reflect.Uint64] = "number"
	kinds[reflect.Float32] = "number"
	kinds[reflect.Float64] = "number"

	kinds[reflect.String] = "string"

	result.kinds = kinds

	result.Indent = "    "
	result.CreateConstructor = true

	return result
}

func deepFields(typeOf reflect.Type) []reflect.StructField {
	fields := make([]reflect.StructField, 0)

	if typeOf.Kind() == reflect.Ptr {
		typeOf = typeOf.Elem()
	}

	if typeOf.Kind() != reflect.Struct {
		return fields
	}

	for i := 0; i < typeOf.NumField(); i++ {
		f := typeOf.Field(i)

		kind := f.Type.Kind()
		if f.Anonymous && kind == reflect.Struct {
			fields = append(fields, deepFields(f.Type)...)
		} else if f.Anonymous && kind == reflect.Ptr && f.Type.Elem().Kind() == reflect.Struct {
			fields = append(fields, deepFields(f.Type.Elem())...)
		} else {
			fields = append(fields, f)
		}
	}

	return fields
}

func (ts TypeScriptify) logf(depth int, s string, args ...interface{}) {
	if !ts.Quiet {
		fmt.Printf(strings.Repeat("   ", depth)+s+"\n", args...)
	}
}

func (t *TypeScriptify) AddFunction(funcDef TypeScriptFunction) *TypeScriptify {
	t.functions = append(t.functions, funcDef)
	return t
}

func (t *TypeScriptify) AddType(typeOf reflect.Type) *TypeScriptify {
	t.structTypes = append(t.structTypes, StructType{Type: typeOf})
	return t
}

func (t *typeScriptClassBuilder) AddMapField(fieldName string, field reflect.StructField) {
	keyType := field.Type.Key()
	valueType := field.Type.Elem()
	valueTypeName := valueType.Name()
	if name, ok := t.types[valueType.Kind()]; ok {
		valueTypeName = name
	}
	if valueType.Kind() == reflect.Array || valueType.Kind() == reflect.Slice {
		valueTypeName = valueType.Elem().Name() + "[]"
	}
	if valueType.Kind() == reflect.Ptr {
		valueTypeName = valueType.Elem().Name()
	}
	strippedFieldName := strings.ReplaceAll(fieldName, "?", "")

	keyTypeStr := keyType.Name()

	if valueType.Kind() == reflect.Struct {
		t.fields = append(t.fields, fmt.Sprintf("%s%s: {[key: %s]: %s};", t.indent, fieldName, keyTypeStr, t.prefix+valueTypeName))
		t.constructorBody = append(t.constructorBody, fmt.Sprintf("%s%sthis.%s = this.convertValues(source[\"%s\"], %s, true);", t.indent, t.indent, strippedFieldName, strippedFieldName, t.prefix+valueTypeName+t.suffix))
	} else {
		t.fields = append(t.fields, fmt.Sprintf("%s%s: {[key: %s]: %s};", t.indent, fieldName, keyTypeStr, valueTypeName))
		t.constructorBody = append(t.constructorBody, fmt.Sprintf("%s%sthis.%s = source[\"%s\"];", t.indent, t.indent, strippedFieldName, strippedFieldName))
	}
}

func (t *TypeScriptify) AddEnum(values interface{}) *TypeScriptify {
	if t.enums == nil {
		t.enums = map[reflect.Type][]enumElement{}
	}
	items := reflect.ValueOf(values)
	if items.Kind() != reflect.Slice {
		panic(fmt.Sprintf("Values for %T isn't a slice", values))
	}

	var elements []enumElement
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i)

		var el enumElement
		if item.Kind() == reflect.Struct {
			r := reflector.New(item.Interface())
			val, err := r.Field("Value").Get()
			if err != nil {
				panic(fmt.Sprint("missing Type field in ", item.Type().String()))
			}
			name, err := r.Field("TSName").Get()
			if err != nil {
				panic(fmt.Sprint("missing TSName field in ", item.Type().String()))
			}
			el.value = val
			el.name = name.(string)
		} else {
			el.value = item.Interface()
			if tsNamer, is := item.Interface().(TSNamer); is {
				el.name = tsNamer.TSName()
			} else {
				panic(fmt.Sprint(item.Type().String(), " has no TSName method"))
			}
		}

		elements = append(elements, el)
	}
	ty := reflect.TypeOf(elements[0].value)
	t.enums[ty] = elements
	t.enumTypes = append(t.enumTypes, EnumType{Type: ty})

	return t
}

func (t *TypeScriptify) Convert(customCode map[string]string) (string, error) {
	t.alreadyConverted = make(map[reflect.Type]bool)
	depth := 0

	result := ""
	if len(t.customImports) > 0 {
		// Put the custom imports, i.e.: `import Decimal from 'decimal.js'`
		for _, cimport := range t.customImports {
			result += cimport + "\n"
		}
	}

	for _, enumTyp := range t.enumTypes {
		elements := t.enums[enumTyp.Type]
		typeScriptCode, err := t.convertEnum(depth, enumTyp.Type, elements)
		if err != nil {
			return "", err
		}
		result += "\n" + strings.Trim(typeScriptCode, " "+t.Indent+"\r\n") + "\n"
	}

	for _, strctTyp := range t.structTypes {
		typeScriptCode, err := t.convertType(depth, strctTyp.Type, customCode)
		if err != nil {
			return "", err
		}
		result += "\n" + strings.Trim(typeScriptCode, " "+t.Indent+"\r\n") + "\n"
	}

	for _, funcDef := range t.functions {
		typeScriptCode, err := t.convertFunction(depth, funcDef)
		if err != nil {
			return "", err
		}
		result += "\n" + strings.Trim(typeScriptCode, " "+t.Indent+"\r\n") + "\n"
	}

	return result, nil
}

func (t *TypeScriptify) convertFunction(depth int, funcDef TypeScriptFunction) (string, error) {
	t.logf(depth, "Converting function %s", funcDef.Name)
	if funcDef.Name == "" {
		return "", fmt.Errorf("function has an empty name")
	}

	// Right now we can spit out multiple defintions for potentially clashing functions

	result := ""
	// Build the list of parameters to be joined
	params := []string{}
	for _, param := range funcDef.Parameters {
		params = append(
			params,
			fmt.Sprintf("%s: %s", param.Name, param.Type),
		)
	}
	asyncString := ""
	if funcDef.IsAsync {
		asyncString = "async "
	}

	if funcDef.ReturnType == "" {
		funcDef.ReturnType = "void"
	}

	exportString := ""
	// Kind of unclear, but if we don't want to export, don't write this string
	if !funcDef.DontExport {
		exportString = "export "
	}

	lines := ""
	for _, line := range funcDef.Body {
		lines += indentLines(line, 1) + "\n"
	}

	funcCode := fmt.Sprintf(
		"%s%sfunction %s(%s): %s {\n%s\n}\n",
		exportString,
		asyncString,
		funcDef.Name,
		strings.Join(params, ", "),
		funcDef.ReturnType,
		lines,
	)
	result += funcCode
	return result, nil
}

type TSNamer interface {
	TSName() string
}

func (t *TypeScriptify) convertEnum(depth int, typeOf reflect.Type, elements []enumElement) (string, error) {
	t.logf(depth, "Converting enum %s", typeOf.String())
	if _, found := t.alreadyConverted[typeOf]; found { // Already converted
		return "", nil
	}
	t.alreadyConverted[typeOf] = true

	entityName := t.Prefix + typeOf.Name() + t.Suffix
	result := "enum " + entityName + " {\n"

	for _, val := range elements {
		result += fmt.Sprintf("%s%s = %#v,\n", t.Indent, val.name, val.value)
	}

	result += "}"

	if !t.DontExport {
		result = "export " + result
	}

	return result, nil
}

func (t *TypeScriptify) getFieldOptions(structType reflect.Type, field reflect.StructField) TypeOptions {
	// By default use options defined by tags:
	opts := TypeOptions{
		TSTransform: field.Tag.Get(tsTransformTag),
		TSType:      field.Tag.Get(tsType),
		TSDoc:       field.Tag.Get(tsDocTag),
	}

	overrides := []TypeOptions{}

	// But there is maybe an struct-specific override:
	for _, strct := range t.structTypes {
		if strct.FieldOptions == nil {
			continue
		}
		if strct.Type == structType {
			if fldOpts, found := strct.FieldOptions[field.Type]; found {
				overrides = append(overrides, fldOpts)
			}
		}
	}

	if fldOpts, found := t.fieldTypeOptions[field.Type]; found {
		overrides = append(overrides, fldOpts)
	}

	for _, o := range overrides {
		if o.TSTransform != "" {
			opts.TSTransform = o.TSTransform
		}
		if o.TSType != "" {
			opts.TSType = o.TSType
		}
	}

	return opts
}

func (t *TypeScriptify) getJSONFieldName(field reflect.StructField, isPtr bool) string {
	jsonFieldName := field.Name
	tag := jsonTag
	if t.CustomJsonTag != "" {
		tag = t.CustomJsonTag
	}
	jsonTag := field.Tag.Get(tag)
	validateTag := field.Tag.Get(validateTagName)

	markedAsRequired := false
	hasOmitEmpty := false
	ignored := false

	// We've found a json tag, handle this
	if len(jsonTag) > 0 {
		jsonTagParts := strings.Split(jsonTag, ",")
		if len(jsonTagParts) > 0 {
			jsonFieldName = strings.Trim(jsonTagParts[0], t.Indent)
		}

		for _, t := range jsonTagParts {
			if t == "" {
				break
			}
			if t == "omitempty" {
				hasOmitEmpty = true
				break
			}
			if t == "-" {
				ignored = true
				break
			}
		}

	}

	// We've found a validator tag, see if it's marked as required
	if len(validateTag) > 0 {
		for _, t := range strings.Split(validateTag, ",") {
			if t == "" {
				break
			}
			if t == "required" {
				markedAsRequired = true
				break
			}
		}
	}

	// How do we want to deal with this? There's potentially conflicting instructions?
	if !ignored && isPtr || hasOmitEmpty || !markedAsRequired {
		jsonFieldName = fmt.Sprintf("%s?", jsonFieldName)
	}

	return jsonFieldName
}

func (t *TypeScriptify) convertType(depth int, typeOf reflect.Type, customCode map[string]string) (string, error) {
	if _, found := t.alreadyConverted[typeOf]; found { // Already converted
		return "", nil
	}
	t.logf(depth, "Converting type %s", typeOf.String())

	t.alreadyConverted[typeOf] = true

	entityName := t.Prefix + typeOf.Name() + t.Suffix
	result := ""
	if t.CreateInterface {
		result += fmt.Sprintf("interface %s {\n", entityName)
	} else {
		result += fmt.Sprintf("class %s {\n", entityName)
	}

	if !t.DontExport {
		result = "export " + result
	}
	builder := typeScriptClassBuilder{
		types:  t.kinds,
		indent: t.Indent,
		prefix: t.Prefix,
		suffix: t.Suffix,
	}

	fields := deepFields(typeOf)
	for _, field := range fields {
		isPtr := field.Type.Kind() == reflect.Ptr
		if isPtr {
			field.Type = field.Type.Elem()
		}
		jsonFieldName := t.getJSONFieldName(field, isPtr)
		if len(jsonFieldName) == 0 || jsonFieldName == "-" {
			continue
		}

		var err error
		fldOpts := t.getFieldOptions(typeOf, field)
		if fldOpts.TSDoc != "" {
			builder.addFieldDefinitionLine("/** " + fldOpts.TSDoc + " */")
		}
		if fldOpts.TSTransform != "" {
			t.logf(depth, "- simple field %s.%s", typeOf.Name(), field.Name)
			err = builder.AddSimpleField(jsonFieldName, field, fldOpts)
		} else if _, isEnum := t.enums[field.Type]; isEnum {
			t.logf(depth, "- enum field %s.%s", typeOf.Name(), field.Name)
			builder.AddEnumField(jsonFieldName, field)
		} else if fldOpts.TSType != "" { // Struct:
			t.logf(depth, "- simple field %s.%s", typeOf.Name(), field.Name)
			err = builder.AddSimpleField(jsonFieldName, field, fldOpts)
		} else if field.Type.Kind() == reflect.Struct { // Struct:
			t.logf(depth, "- struct %s.%s (%s)", typeOf.Name(), field.Name, field.Type.String())
			typeScriptChunk, err := t.convertType(depth+1, field.Type, customCode)
			if err != nil {
				return "", err
			}
			if typeScriptChunk != "" {
				result = typeScriptChunk + "\n" + result
			}
			builder.AddStructField(jsonFieldName, field)
		} else if field.Type.Kind() == reflect.Map {
			t.logf(depth, "- map field %s.%s", typeOf.Name(), field.Name)
			// Also convert map key types if needed
			var keyTypeToConvert reflect.Type
			switch field.Type.Key().Kind() {
			case reflect.Struct:
				keyTypeToConvert = field.Type.Key()
			case reflect.Ptr:
				keyTypeToConvert = field.Type.Key().Elem()
			}
			if keyTypeToConvert != nil {
				typeScriptChunk, err := t.convertType(depth+1, keyTypeToConvert, customCode)
				if err != nil {
					return "", err
				}
				if typeScriptChunk != "" {
					result = typeScriptChunk + "\n" + result
				}
			}
			// Also convert map value types if needed
			var valueTypeToConvert reflect.Type
			switch field.Type.Elem().Kind() {
			case reflect.Struct:
				valueTypeToConvert = field.Type.Elem()
			case reflect.Ptr:
				valueTypeToConvert = field.Type.Elem().Elem()
			}
			if valueTypeToConvert != nil {
				typeScriptChunk, err := t.convertType(depth+1, valueTypeToConvert, customCode)
				if err != nil {
					return "", err
				}
				if typeScriptChunk != "" {
					result = typeScriptChunk + "\n" + result
				}
			}

			builder.AddMapField(jsonFieldName, field)
		} else if field.Type.Kind() == reflect.Slice || field.Type.Kind() == reflect.Array { // Slice:
			if field.Type.Elem().Kind() == reflect.Ptr { //extract ptr type
				field.Type = field.Type.Elem()
			}

			arrayDepth := 1
			for field.Type.Elem().Kind() == reflect.Slice { // Slice of slices:
				field.Type = field.Type.Elem()
				arrayDepth++
			}

			if field.Type.Elem().Kind() == reflect.Struct { // Slice of structs:
				t.logf(depth, "- struct slice %s.%s (%s)", typeOf.Name(), field.Name, field.Type.String())
				typeScriptChunk, err := t.convertType(depth+1, field.Type.Elem(), customCode)
				if err != nil {
					return "", err
				}
				if typeScriptChunk != "" {
					result = typeScriptChunk + "\n" + result
				}
				builder.AddArrayOfStructsField(jsonFieldName, field, arrayDepth)
			} else { // Slice of simple fields:
				t.logf(depth, "- slice field %s.%s", typeOf.Name(), field.Name)
				err = builder.AddSimpleArrayField(jsonFieldName, field, arrayDepth, fldOpts)
			}
		} else { // Simple field:
			t.logf(depth, "- simple field %s.%s", typeOf.Name(), field.Name)
			err = builder.AddSimpleField(jsonFieldName, field, fldOpts)
		}
		if err != nil {
			return "", err
		}
	}

	result += strings.Join(builder.fields, "\n") + "\n"
	if !t.CreateInterface {
		constructorBody := strings.Join(builder.constructorBody, "\n")
		needsConvertValue := strings.Contains(constructorBody, "this.convertValues")
		if t.CreateConstructor {
			result += fmt.Sprintf("\n%sconstructor(source: any = {}) {\n", t.Indent)
			result += t.Indent + t.Indent + "if ('string' === typeof source) source = JSON.parse(source);\n"
			result += constructorBody + "\n"
			result += fmt.Sprintf("%s}\n", t.Indent)
		}
		if needsConvertValue && (t.CreateConstructor) {
			result += "\n" + indentLines(strings.ReplaceAll(tsConvertValuesFunc, "\t", t.Indent), 1) + "\n"
		}
	}

	if customCode != nil {
		code := customCode[entityName]
		if len(code) != 0 {
			result += t.Indent + "//[" + entityName + ":]\n" + code + "\n\n" + t.Indent + "//[end]\n"
		}
	}

	result += "}"

	return result, nil
}

type typeScriptClassBuilder struct {
	types                map[reflect.Kind]string
	indent               string
	fields               []string
	createFromMethodBody []string
	constructorBody      []string
	prefix, suffix       string
}

func (t *typeScriptClassBuilder) AddSimpleArrayField(fieldName string, field reflect.StructField, arrayDepth int, opts TypeOptions) error {
	fieldType, kind := field.Type.Elem().Name(), field.Type.Elem().Kind()
	typeScriptType := t.types[kind]

	if len(fieldName) > 0 {
		strippedFieldName := strings.ReplaceAll(fieldName, "?", "")
		if len(opts.TSType) > 0 {
			t.addField(fieldName, opts.TSType)
			t.addInitializerFieldLine(strippedFieldName, fmt.Sprintf("source[\"%s\"]", strippedFieldName))
			return nil
		} else if len(typeScriptType) > 0 {
			t.addField(fieldName, fmt.Sprint(typeScriptType, strings.Repeat("[]", arrayDepth)))
			t.addInitializerFieldLine(strippedFieldName, fmt.Sprintf("source[\"%s\"]", strippedFieldName))
			return nil
		}
	}

	return fmt.Errorf("cannot find type for %s (%s/%s)", kind.String(), fieldName, fieldType)
}

func (t *typeScriptClassBuilder) AddSimpleField(fieldName string, field reflect.StructField, opts TypeOptions) error {
	fieldType, kind := field.Type.Name(), field.Type.Kind()

	typeScriptType := t.types[kind]
	if len(opts.TSType) > 0 {
		typeScriptType = opts.TSType
	}

	if len(typeScriptType) > 0 && len(fieldName) > 0 {
		strippedFieldName := strings.ReplaceAll(fieldName, "?", "")
		t.addField(fieldName, typeScriptType)
		if opts.TSTransform == "" {
			t.addInitializerFieldLine(strippedFieldName, fmt.Sprintf("source[\"%s\"]", strippedFieldName))
		} else {
			val := fmt.Sprintf(`source["%s"]`, strippedFieldName)
			expression := strings.Replace(opts.TSTransform, "__VALUE__", val, -1)
			t.addInitializerFieldLine(strippedFieldName, expression)
		}
		return nil
	}

	return fmt.Errorf("cannot find type for %s (%s/%s)", kind.String(), fieldName, fieldType)
}

func (t *typeScriptClassBuilder) AddEnumField(fieldName string, field reflect.StructField) {
	fieldType := field.Type.Name()
	t.addField(fieldName, t.prefix+fieldType+t.suffix)
	strippedFieldName := strings.ReplaceAll(fieldName, "?", "")
	t.addInitializerFieldLine(strippedFieldName, fmt.Sprintf("source[\"%s\"]", strippedFieldName))
}

func (t *typeScriptClassBuilder) AddStructField(fieldName string, field reflect.StructField) {
	fieldType := field.Type.Name()
	strippedFieldName := strings.ReplaceAll(fieldName, "?", "")
	t.addField(fieldName, t.prefix+fieldType+t.suffix)
	t.addInitializerFieldLine(strippedFieldName, fmt.Sprintf("this.convertValues(source[\"%s\"], %s)", strippedFieldName, t.prefix+fieldType+t.suffix))
}

func (t *typeScriptClassBuilder) AddArrayOfStructsField(fieldName string, field reflect.StructField, arrayDepth int) {
	fieldType := field.Type.Elem().Name()
	strippedFieldName := strings.ReplaceAll(fieldName, "?", "")
	t.addField(fieldName, fmt.Sprint(t.prefix+fieldType+t.suffix, strings.Repeat("[]", arrayDepth)))
	t.addInitializerFieldLine(strippedFieldName, fmt.Sprintf("this.convertValues(source[\"%s\"], %s)", strippedFieldName, t.prefix+fieldType+t.suffix))
}

func (t *typeScriptClassBuilder) addInitializerFieldLine(fld, initializer string) {
	t.createFromMethodBody = append(t.createFromMethodBody, fmt.Sprint(t.indent, t.indent, "result.", fld, " = ", initializer, ";"))
	t.constructorBody = append(t.constructorBody, fmt.Sprint(t.indent, t.indent, "this.", fld, " = ", initializer, ";"))
}

func (t *typeScriptClassBuilder) addFieldDefinitionLine(line string) {
	t.fields = append(t.fields, t.indent+line)
}

func (t *typeScriptClassBuilder) addField(fld, fldType string) {
	t.fields = append(t.fields, fmt.Sprint(t.indent, fld, ": ", fldType, ";"))
}

func indentLines(str string, i int) string {
	lines := strings.Split(str, "\n")
	for n := range lines {
		lines[n] = strings.Repeat("\t", i) + lines[n]
	}
	return strings.Join(lines, "\n")
}
