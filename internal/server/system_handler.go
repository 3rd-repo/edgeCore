package server

import (
	"os"
	"time"

	"github.com/anviod/edgeCore/internal/model"

	"github.com/gofiber/fiber/v2"
)

func (s *Server) getSystemConfig(c *fiber.Ctx) error {
	cfg := s.sm.GetConfig()
	return c.JSON(cfg)
}

func (s *Server) updateSystemConfig(c *fiber.Ctx) error {
	var newConfig model.SystemConfig
	if err := c.BodyParser(&newConfig); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	previousPort := s.GetListenPort()

	if err := s.sm.UpdateConfig(newConfig); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	newPort := newConfig.Hostname.HTTPPort
	if newPort == 0 {
		newPort = 8080
	}
	if previousPort != 0 && newPort != previousPort {
		if err := s.SwitchPort(newPort); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Config saved but failed to switch HTTP port: " + err.Error()})
		}
	}

	return c.JSON(fiber.Map{"status": "success", "message": "System configuration updated"})
}

func (s *Server) getNetworkInterfaces(c *fiber.Ctx) error {
	interfaces, err := s.sm.GetNetworkInterfaces()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(interfaces)
}

func (s *Server) getRoutes(c *fiber.Ctx) error {
	routes, err := s.sm.GetRoutes()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(routes)
}

func (s *Server) addRoute(c *fiber.Ctx) error {
	var route model.StaticRoute
	if err := c.BodyParser(&route); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := s.sm.AddRoute(route); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(route)
}

func (s *Server) deleteRoute(c *fiber.Ctx) error {
	var route model.StaticRoute
	if err := c.BodyParser(&route); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := s.sm.DeleteRoute(route); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"status": "success"})
}

func (s *Server) getNetworkInfo(c *fiber.Ctx) error {
	info := s.sm.GetNetworkBackendInfo()
	return c.JSON(info)
}

func (s *Server) getHostnameAccessStatus(c *fiber.Ctx) error {
	return c.JSON(s.sm.GetHostnameAccessStatus())
}

func (s *Server) checkConnectivity(c *fiber.Ctx) error {
	var targets []model.ConnectivityTarget
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&targets); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
		}
	}
	report, err := s.sm.ValidateConnectivity(targets)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(report)
}

func (s *Server) handleRestart(c *fiber.Ctx) error {
	// P0 数据丢失防护（2026-10-13 审查）：
	// 此前这里 os.Exit(0) 硬退出，跳过 pipeline 最终 drain、MQTT flush、
	// ShadowIngress flushBuffer、store.Close 全部 defer，缓冲数据全部丢失。
	// 现在通知 main 的统一关停路径（与 SIGTERM 完全一致），响应先返回。
	if s.restartSignal != nil {
		go func() {
			time.Sleep(1 * time.Second) // 让 HTTP 响应先送达
			select {
			case s.restartSignal <- struct{}{}:
			default:
			}
		}()
		return c.JSON(fiber.Map{"status": "success", "message": "System is restarting..."})
	}
	// 无回调接线（单测等场景）：退回旧行为，但至少同步执行已注册的清理
	go func() {
		time.Sleep(1 * time.Second)
		os.Exit(0)
	}()
	return c.JSON(fiber.Map{"status": "success", "message": "System is restarting..."})
}
