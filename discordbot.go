package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/tr4cks/power/modules"
)

type DiscordBot struct {
	config *DiscordBotConfig
	module modules.Module

	logger             zerolog.Logger
	session            *discordgo.Session
	registeredCommands []*discordgo.ApplicationCommand
}

type DiscordBotConfig struct {
	BotToken string `yaml:"bot-token" validate:"required"`
	GuildId  string `yaml:"guild-id"`
}

func (d *DiscordBot) Start() error {
	err := d.session.Open()
	if err != nil {
		return fmt.Errorf("cannot open the session: %w", err)
	}

	d.logger.Info().Msg("Adding commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := d.session.ApplicationCommandCreate(d.session.State.User.ID, d.config.GuildId, v)
		if err != nil {
			d.logger.Panic().Err(err).Msg(fmt.Sprintf("Cannot create '%v' command: %v", v.Name, err))
		}
		registeredCommands[i] = cmd
	}
	d.registeredCommands = registeredCommands

	return nil
}

func (d *DiscordBot) Stop() {
	d.logger.Info().Msg("Removing commands...")

	for _, v := range d.registeredCommands {
		err := d.session.ApplicationCommandDelete(d.session.State.User.ID, d.config.GuildId, v.ID)

		if err != nil {
			log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
		}
	}

	err := d.session.Close()
	if err != nil {
		d.logger.Error().Err(err).Msg("Unable to close the session")
	}

	d.logger.Info().Msg("Gracefully shutting down")
}

func (d *DiscordBot) serverStatusHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	logger := d.logger.With().Str("username", i.Member.User.Username).Logger()
	logger.Info().Msg("A user tries to check the server status")

	sendFollowup := func(content string) {
		_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: content,
		})
		if err != nil {
			logger.Error().Err(err).Msg("Failed to send follow-up message")
		}
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "⏳ Connecting to the server… Please wait",
		},
	})
	if err != nil {
		logger.Error().Err(err).Msg("Failed to send interaction response")
		return
	}

	powerState, ledState := d.module.State()

	for _, state := range []struct {
		err error
		msg string
	}{
		{powerState.Err, "Failed to retrieve POWER state"},
		{ledState.Err, "Failed to retrieve LED state"},
	} {
		if state.err != nil {
			logger.Error().Err(state.err).Msg(state.msg)
			sendFollowup("❌ Oops! Something went wrong while starting the server")
			return
		}
	}

	if powerState.Value || ledState.Value {
		sendFollowup("🌞 Server is awake!")
	} else {
		sendFollowup("💤 Server is asleep!")
	}
}

func (d *DiscordBot) powerOnHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	logger := d.logger.With().Str("username", i.Member.User.Username).Logger()
	logger.Info().Msg("A user attempts to switch on the server")

	sendFollowup := func(content string) {
		_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: content,
		})
		if err != nil {
			logger.Error().Err(err).Msg("Failed to send follow-up message")
		}
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "⏳ Connecting to the server… Please wait",
		},
	})
	if err != nil {
		logger.Error().Err(err).Msg("Failed to send interaction response")
		return
	}

	powerState, ledState := d.module.State()

	for _, state := range []struct {
		err error
		msg string
	}{
		{powerState.Err, "Failed to retrieve POWER state"},
		{ledState.Err, "Failed to retrieve LED state"},
	} {
		if state.err != nil {
			logger.Error().Err(state.err).Msg(state.msg)
			sendFollowup("❌ Oops! Something went wrong while starting the server")
			return
		}
	}

	if powerState.Value || ledState.Value {
		logger.Info().Msg("The server is already switched on")
		sendFollowup("✅ The server is already running!")
		return
	}

	err = d.module.PowerOn()
	if err != nil {
		logger.Error().Err(err).Msg("A problem occurred when switching on the server")
		sendFollowup("❌ Oops! Something went wrong while starting the server")
		return
	}
	logger.Info().Msg("Server switched on")
	sendFollowup("✨ The server is waking up! It’ll be ready soon")
}

func (d *DiscordBot) powerOffHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	logger := d.logger.With().Str("username", i.Member.User.Username).Logger()
	logger.Info().Msg("A user attempts to switch off the server")

	sendFollowup := func(content string) {
		_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: content,
		})
		if err != nil {
			logger.Error().Err(err).Msg("Failed to send follow-up message")
		}
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "⏳ Connecting to the server… Please wait",
		},
	})
	if err != nil {
		logger.Error().Err(err).Msg("Failed to send interaction response")
		return
	}

	powerState, ledState := d.module.State()

	for _, state := range []struct {
		err error
		msg string
	}{
		{powerState.Err, "Failed to retrieve POWER state"},
		{ledState.Err, "Failed to retrieve LED state"},
	} {
		if state.err != nil {
			logger.Error().Err(state.err).Msg(state.msg)
			sendFollowup("❌ Oops! Something went wrong while stopping the server")
			return
		}
	}

	if !powerState.Value && !ledState.Value {
		logger.Info().Msg("The server is already switched off")
		sendFollowup("✅ The server is already stopped!")
		return
	}

	err = d.module.PowerOff()
	if err != nil {
		logger.Error().Err(err).Msg("A problem occurred when switching off the server")
		sendFollowup("❌ Oops! Something went wrong while stopping the server")
		return
	}
	logger.Info().Msg("Server switched off")
	sendFollowup("🛌 The server is shutting down!")
}

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "server_status",
		Description: "Provides the current status of the server",
	},
	{
		Name:        "power_on",
		Description: "Turns the server on",
	},
	{
		Name:        "power_off",
		Description: "Turns the server off",
		DefaultMemberPermissions: func() *int64 {
			perms := int64(discordgo.PermissionAdministrator)
			return &perms
		}(),
	},
}

func NewDiscordBot(config *DiscordBotConfig, module modules.Module) (*DiscordBot, error) {
	var outputWriter io.Writer = os.Stderr
	if gin.Mode() != "release" {
		outputWriter = zerolog.ConsoleWriter{Out: os.Stderr}
	}
	logger := zerolog.
		New(outputWriter).
		With().
		Timestamp().
		Str("scope", "discord").
		Logger()

	session, err := discordgo.New("Bot " + config.BotToken)
	if err != nil {
		return nil, fmt.Errorf("invalid bot parameters: %w", err)
	}

	bot := &DiscordBot{config, module, logger, session, nil}

	commandHandlers := map[string]func(*discordgo.Session, *discordgo.InteractionCreate){
		"server_status": bot.serverStatusHandler,
		"power_on":      bot.powerOnHandler,
		"power_off":     bot.powerOffHandler,
	}

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})

	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		logger.Info().
			Str("discriminator", s.State.User.Discriminator).
			Str("username", s.State.User.Username).
			Msg(fmt.Sprintf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator))
	})

	return bot, nil
}
