package validate

import "github.com/go-playground/validator/v10"

// V is the shared validator instance.
var V = validator.New(validator.WithRequiredStructEnabled())
