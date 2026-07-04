package httpx

import (
	"github.com/gofiber/fiber/v2"
)

func JSON(c *fiber.Ctx, status int, data interface{}) error {
	return c.Status(status).JSON(fiber.Map{
		"data":  data,
		"error": nil,
	})
}

func EmptyJSON(c *fiber.Ctx, status int) error {
	return JSON(c, status, nil)
}

func Error(c *fiber.Ctx, status int, err error) error {
	return c.Status(status).JSON(fiber.Map{
		"data":  nil,
		"error": err.Error(),
	})
}

func SetupHealthRoutes(a *fiber.App) {
	health := func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	}

	a.Get("/", health)
	a.Get("/health", health)
}
