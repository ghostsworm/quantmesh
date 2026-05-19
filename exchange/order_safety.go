package exchange

import (
	"errors"
	"fmt"
	"strconv"
)

func orderIDString(orderID int64) string {
	return strconv.FormatInt(orderID, 10)
}

func appendOrderOpError(errs []error, orderID string, err error) []error {
	if err == nil {
		return errs
	}
	if orderID == "" {
		return append(errs, err)
	}
	return append(errs, fmt.Errorf("order %s: %w", orderID, err))
}

func joinOrderOpErrors(operation string, errs []error) error {
	filtered := errs[:0]
	for _, err := range errs {
		if err != nil {
			filtered = append(filtered, err)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return fmt.Errorf("%s failed: %w", operation, errors.Join(filtered...))
}
