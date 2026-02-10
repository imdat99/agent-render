package pg

import (
	"context"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"picpic.render/internal/core/domain"
	"picpic.render/internal/core/ports"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(dsn string) (ports.JobRepository, ports.AgentRepository, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}

	// Auto migrate
	if err := db.AutoMigrate(&domain.Job{}, &domain.Agent{}); err != nil {
		return nil, nil, err
	}

	return &Repository{db: db}, &Repository{db: db}, nil
}

// Job methods
func (r *Repository) Create(ctx context.Context, job *domain.Job) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *Repository) GetJob(ctx context.Context, id string) (*domain.Job, error) {
	var job domain.Job
	if err := r.db.WithContext(ctx).First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *Repository) Update(ctx context.Context, job *domain.Job) error {
	return r.db.WithContext(ctx).Save(job).Error
}

func (r *Repository) ListJobs(ctx context.Context, offset, limit int) ([]*domain.Job, error) {
	var jobs []*domain.Job
	if err := r.db.WithContext(ctx).Order("created_at desc").Offset(offset).Limit(limit).Find(&jobs).Error; err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *Repository) ListJobsByAgent(ctx context.Context, agentID int64, offset, limit int) ([]*domain.Job, error) {
	var jobs []*domain.Job
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Order("created_at desc").Offset(offset).Limit(limit).Find(&jobs).Error; err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *Repository) CountJobs(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.Job{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Repository) CountActiveJobsByAgent(ctx context.Context, agentID int64) (int64, error) {
	var count int64
	// Active jobs are those with status 'running'
	if err := r.db.WithContext(ctx).Model(&domain.Job{}).Where("agent_id = ? AND status = ?", agentID, domain.JobStatusRunning).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Repository) CountJobsByAgent(ctx context.Context, agentID int64) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.Job{}).Where("agent_id = ?", agentID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Agent methods
func (r *Repository) CreateOrUpdate(ctx context.Context, agent *domain.Agent) error {
	// Upsert based on ID
	// If ID is 0, create. But generic woodpecker agents might not have stable IDs?
	// The problem is Agent ID assignment.
	// For now, let's assume we register and get an ID, subsequent updates use that ID.
	if agent.ID == 0 {
		return r.db.WithContext(ctx).Create(agent).Error
	}
	return r.db.WithContext(ctx).Save(agent).Error
}

func (r *Repository) UpdateHeartbeat(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Model(&domain.Agent{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"last_heartbeat": gorm.Expr("NOW()"),
			"updated_at":     gorm.Expr("NOW()"),
		}).Error
}

func (r *Repository) GetAgent(ctx context.Context, id int64) (*domain.Agent, error) {
	var agent domain.Agent
	if err := r.db.WithContext(ctx).First(&agent, id).Error; err != nil {
		return nil, err
	}
	return &agent, nil
}

func (r *Repository) GetAgentByName(ctx context.Context, name string) (*domain.Agent, error) {
	var agent domain.Agent
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&agent).Error; err != nil {
		return nil, err
	}
	return &agent, nil
}

func (r *Repository) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	var agents []*domain.Agent
	if err := r.db.WithContext(ctx).Find(&agents).Error; err != nil {
		return nil, err
	}
	return agents, nil
}

// DB returns the underlying gorm DB instance
func (r *Repository) DB() (*gorm.DB, error) {
	return r.db, nil
}
