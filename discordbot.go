package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

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

func (d *DiscordBot) powerOnHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	logger := d.logger.With().Str("username", i.Member.User.Username).Logger()
	logger.Info().Msg("A user attempts to switch on the server")

	var deferStack []func()

	defer func() {
		if len(deferStack) > 0 {
			time.Sleep(10 * time.Second)
		}
		for i := len(deferStack) - 1; i >= 0; i-- {
			deferStack[i]()
		}
	}()

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "Server startup in progress... Please wait",
		},
	})

	deferStack = append(deferStack, func() {
		s.InteractionResponseDelete(i.Interaction)
	})

	powerState, ledState := d.module.State()

	if powerState.Err != nil {
		logger.Error().Err(powerState.Err).Msg("Failed to retrieve POWER state")
		followupMsg, _ := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "A problem occurred when switching on the server",
		})
		deferStack = append(deferStack, func() {
			s.FollowupMessageDelete(i.Interaction, followupMsg.ID)
		})
		return
	}
	if ledState.Err != nil {
		logger.Error().Err(ledState.Err).Msg("Failed to retrieve LED state")
		followupMsg, _ := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "A problem occurred when switching on the server",
		})
		deferStack = append(deferStack, func() {
			s.FollowupMessageDelete(i.Interaction, followupMsg.ID)
		})
		return
	}

	if powerState.Value || ledState.Value {
		logger.Info().Msg("The server is already switched on")
		followupMsg, _ := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "The server is already switched on",
		})
		deferStack = append(deferStack, func() {
			s.FollowupMessageDelete(i.Interaction, followupMsg.ID)
		})
		return
	}

	err := d.module.PowerOn()
	if err != nil {
		logger.Error().Err(err).Msg("A problem occurred when switching on the server")
		followupMsg, _ := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "A problem occurred when switching on the server",
		})
		deferStack = append(deferStack, func() {
			s.FollowupMessageDelete(i.Interaction, followupMsg.ID)
		})
		return
	}
	logger.Info().Msg("Server switched on")

	followupMsg, _ := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Flags:   discordgo.MessageFlagsEphemeral,
		Content: "The server is now starting! This may take a few minutes",
	})
	deferStack = append(deferStack, func() {
		s.FollowupMessageDelete(i.Interaction, followupMsg.ID)
	})
}

var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "power_on",
		Description: "Turns the server on",
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
		"power_on": bot.powerOnHandler,
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
