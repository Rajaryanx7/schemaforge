package schemaforge

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func convertActionValue(value any, dataType string) (any, error) {
	if value == nil {
		return nil, nil
	}

	switch dataType {
	case "character varying", "character", "text":
		s, ok := value.(string)
		if !ok {
			return nil, errors.New("must be a string")
		}
		if strings.ContainsRune(s, 0) {
			return nil, errors.New("must not contain NUL characters")
		}
		return s, nil

	case "smallint", "integer", "bigint":
		var n json.Number

		switch v := value.(type) {
		case json.Number:
			n = v
		case int:
			n = json.Number(strconv.Itoa(v))
		case int64:
			n = json.Number(strconv.FormatInt(v, 10))
		case float64:
			return nil, errors.New("must be an integer, not a floating-point value")
		default:
			return nil, errors.New("must be an integer")
		}

		bits := 64
		switch dataType {
		case "smallint":
			bits = 16
		case "integer":
			bits = 32
		}

		i, err := strconv.ParseInt(n.String(), 10, bits)
		if err != nil {
			return nil, errors.New("must be a whole number within the column's range")
		}
		return i, nil

	case "boolean":
		b, ok := value.(bool)
		if !ok {
			return nil, errors.New("must be true or false")
		}
		return b, nil

	default:
		return nil, fmt.Errorf("columns of type %q are not supported", dataType)
	}
}
