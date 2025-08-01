package repositories

import (
	"encoding/json"
	"fmt"
	"time"

	"eduqr-backend/internal/models"

	"gorm.io/gorm"
)

type CourseRepository struct {
	db *gorm.DB
}

func NewCourseRepository(db *gorm.DB) *CourseRepository {
	return &CourseRepository{db: db}
}

// GetAllCourses récupère tous les cours avec leurs relations
func (r *CourseRepository) GetAllCourses() ([]models.Course, error) {
	var courses []models.Course
	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").Find(&courses).Error
	return courses, err
}

// GetCourseByID récupère un cours par son ID
func (r *CourseRepository) GetCourseByID(id uint) (*models.Course, error) {
	var course models.Course
	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").First(&course, id).Error
	if err != nil {
		return nil, err
	}
	return &course, nil
}

// CreateCourse crée un nouveau cours
func (r *CourseRepository) CreateCourse(course *models.Course) error {
	// Calculer l'heure de fin
	course.EndTime = course.StartTime.Add(time.Duration(course.Duration) * time.Minute)

	// Vérifier les conflits
	conflicts, err := r.CheckConflicts(course)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("conflits détectés: %v", conflicts)
	}

	return r.db.Create(course).Error
}

// UpdateCourse met à jour un cours existant
func (r *CourseRepository) UpdateCourse(course *models.Course) error {
	// Calculer l'heure de fin
	course.EndTime = course.StartTime.Add(time.Duration(course.Duration) * time.Minute)

	// Vérifier les conflits (en excluant le cours lui-même)
	conflicts, err := r.CheckConflictsExcluding(course.ID, course)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("conflits détectés: %v", conflicts)
	}

	return r.db.Save(course).Error
}

// DeleteCourse supprime un cours
func (r *CourseRepository) DeleteCourse(id uint) error {
	return r.db.Delete(&models.Course{}, id).Error
}

// DeleteRecurringCourses supprime tous les cours d'une série récurrente
func (r *CourseRepository) DeleteRecurringCourses(recurrenceID uint) error {
	// Supprimer d'abord le cours parent
	if err := r.db.Delete(&models.Course{}, recurrenceID).Error; err != nil {
		return err
	}

	// Puis supprimer tous les cours enfants
	return r.db.Where("recurrence_id = ?", recurrenceID).Delete(&models.Course{}).Error
}

// GetCoursesByDateRange récupère les cours dans une plage de dates
func (r *CourseRepository) GetCoursesByDateRange(startDate, endDate time.Time) ([]models.Course, error) {
	var courses []models.Course
	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("start_time >= ? AND start_time <= ?", startDate, endDate).
		Find(&courses).Error
	return courses, err
}

// GetCoursesByRoom récupère les cours d'une salle
func (r *CourseRepository) GetCoursesByRoom(roomID uint) ([]models.Course, error) {
	var courses []models.Course
	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("room_id = ?", roomID).
		Find(&courses).Error
	return courses, err
}

// GetCoursesByRoomAndDate récupère les cours d'une salle pour une date spécifique
func (r *CourseRepository) GetCoursesByRoomAndDate(roomID uint, targetDate time.Time) ([]models.Course, error) {
	var courses []models.Course

	// Calculer le début et la fin de la journée
	startOfDay := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, targetDate.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)

	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("room_id = ? AND start_time >= ? AND start_time < ?", roomID, startOfDay, endOfDay).
		Order("start_time ASC").
		Find(&courses).Error
	return courses, err
}

// GetCoursesByTeacher récupère les cours d'un enseignant
func (r *CourseRepository) GetCoursesByTeacher(teacherID uint) ([]models.Course, error) {
	var courses []models.Course
	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("teacher_id = ?", teacherID).
		Find(&courses).Error
	return courses, err
}

// CheckConflicts vérifie les conflits de réservation pour une salle
func (r *CourseRepository) CheckConflicts(course *models.Course) ([]models.ConflictInfo, error) {
	var conflicts []models.ConflictInfo

	// Récupérer la salle avec ses relations
	var room models.Room
	if err := r.db.Preload("Parent").Preload("Children").First(&room, course.RoomID).Error; err != nil {
		return nil, err
	}

	// Vérifier les conflits pour la salle elle-même
	roomConflicts, err := r.checkRoomConflicts(course.RoomID, course.StartTime, course.EndTime)
	if err != nil {
		return nil, err
	}
	conflicts = append(conflicts, roomConflicts...)

	// Si c'est une salle modulable (parent), vérifier les salles enfants
	if room.IsModular {
		for _, childRoom := range room.Children {
			childConflicts, err := r.checkRoomConflicts(childRoom.ID, course.StartTime, course.EndTime)
			if err != nil {
				return nil, err
			}
			conflicts = append(conflicts, childConflicts...)
		}
	}

	// Si c'est une sous-salle, vérifier la salle parente
	if room.Parent != nil {
		parentConflicts, err := r.checkRoomConflicts(room.Parent.ID, course.StartTime, course.EndTime)
		if err != nil {
			return nil, err
		}
		conflicts = append(conflicts, parentConflicts...)
	}

	return conflicts, nil
}

// CheckConflictsExcluding vérifie les conflits en excluant un cours spécifique
func (r *CourseRepository) CheckConflictsExcluding(excludeID uint, course *models.Course) ([]models.ConflictInfo, error) {
	var conflicts []models.ConflictInfo

	// Récupérer la salle avec ses relations
	var room models.Room
	if err := r.db.Preload("Parent").Preload("Children").First(&room, course.RoomID).Error; err != nil {
		return nil, err
	}

	// Vérifier les conflits pour la salle elle-même (en excluant le cours)
	roomConflicts, err := r.checkRoomConflictsExcluding(excludeID, course.RoomID, course.StartTime, course.EndTime)
	if err != nil {
		return nil, err
	}
	conflicts = append(conflicts, roomConflicts...)

	// Si c'est une salle modulable (parent), vérifier les salles enfants
	if room.IsModular {
		for _, childRoom := range room.Children {
			childConflicts, err := r.checkRoomConflictsExcluding(excludeID, childRoom.ID, course.StartTime, course.EndTime)
			if err != nil {
				return nil, err
			}
			conflicts = append(conflicts, childConflicts...)
		}
	}

	// Si c'est une sous-salle, vérifier la salle parente
	if room.Parent != nil {
		parentConflicts, err := r.checkRoomConflictsExcluding(excludeID, room.Parent.ID, course.StartTime, course.EndTime)
		if err != nil {
			return nil, err
		}
		conflicts = append(conflicts, parentConflicts...)
	}

	return conflicts, nil
}

// checkRoomConflicts vérifie les conflits pour une salle spécifique
func (r *CourseRepository) checkRoomConflicts(roomID uint, startTime, endTime time.Time) ([]models.ConflictInfo, error) {
	var conflicts []models.ConflictInfo

	var existingCourses []models.Course
	err := r.db.Preload("Room").
		Where("room_id = ? AND ((start_time <= ? AND end_time > ?) OR (start_time < ? AND end_time >= ?) OR (start_time >= ? AND end_time <= ?))",
			roomID, startTime, startTime, endTime, endTime, startTime, endTime).
		Find(&existingCourses).Error

	if err != nil {
		return nil, err
	}

	for _, course := range existingCourses {
		conflicts = append(conflicts, models.ConflictInfo{
			Date:       course.StartTime,
			StartTime:  course.StartTime,
			EndTime:    course.EndTime,
			RoomName:   course.Room.Name,
			CourseName: course.Name,
		})
	}

	return conflicts, nil
}

// checkRoomConflictsExcluding vérifie les conflits en excluant un cours spécifique et ses cours récurrents associés
func (r *CourseRepository) checkRoomConflictsExcluding(excludeID uint, roomID uint, startTime, endTime time.Time) ([]models.ConflictInfo, error) {
	var conflicts []models.ConflictInfo

	// Récupérer le cours à exclure pour vérifier s'il est récurrent
	var excludeCourse models.Course
	if err := r.db.First(&excludeCourse, excludeID).Error; err != nil {
		return nil, err
	}

	// Construire la condition WHERE pour exclure le cours et ses cours récurrents associés
	whereCondition := "room_id = ? AND ((start_time <= ? AND end_time > ?) OR (start_time < ? AND end_time >= ?) OR (start_time >= ? AND end_time <= ?))"
	args := []interface{}{roomID, startTime, startTime, endTime, endTime, startTime, endTime}

	// Exclure le cours lui-même
	whereCondition += " AND id != ?"
	args = append(args, excludeID)

	// Si c'est un cours récurrent, exclure aussi tous les cours de la même série
	if excludeCourse.IsRecurring {
		if excludeCourse.RecurrenceID == nil {
			// C'est un cours parent récurrent, exclure tous ses enfants
			whereCondition += " AND (recurrence_id IS NULL OR recurrence_id != ?)"
			args = append(args, excludeID)
		} else {
			// C'est un cours enfant récurrent, exclure le parent et tous les autres enfants
			whereCondition += " AND (recurrence_id IS NULL OR recurrence_id != ?)"
			args = append(args, *excludeCourse.RecurrenceID)
		}
	}

	var existingCourses []models.Course
	err := r.db.Preload("Room").
		Where(whereCondition, args...).
		Find(&existingCourses).Error

	if err != nil {
		return nil, err
	}

	for _, course := range existingCourses {
		conflicts = append(conflicts, models.ConflictInfo{
			Date:       course.StartTime,
			StartTime:  course.StartTime,
			EndTime:    course.EndTime,
			RoomName:   course.Room.Name,
			CourseName: course.Name,
		})
	}

	return conflicts, nil
}

// GenerateRecurringCourses génère les cours récurrents
func (r *CourseRepository) GenerateRecurringCourses(parentCourse *models.Course) error {
	if !parentCourse.IsRecurring || parentCourse.RecurrencePattern == nil || parentCourse.RecurrenceEndDate == nil {
		return fmt.Errorf("cours non récurrent ou paramètres manquants")
	}

	var pattern models.RecurrencePattern
	if err := json.Unmarshal([]byte(*parentCourse.RecurrencePattern), &pattern); err != nil {
		return err
	}

	// Générer les cours pour chaque jour de la semaine spécifié
	currentDate := parentCourse.StartTime
	for currentDate.Before(*parentCourse.RecurrenceEndDate) {
		weekday := currentDate.Weekday().String()

		// Vérifier si ce jour est dans le pattern
		for _, day := range pattern.Days {
			if day == weekday {
				// Éviter de créer un doublon pour la date du cours parent
				if currentDate.Equal(parentCourse.StartTime) {
					// Passer au jour suivant
					currentDate = currentDate.AddDate(0, 0, 1)
					continue
				}

				// Créer un cours pour ce jour
				course := *parentCourse
				course.ID = 0 // Nouveau cours
				course.RecurrenceID = &parentCourse.ID
				course.StartTime = time.Date(
					currentDate.Year(), currentDate.Month(), currentDate.Day(),
					parentCourse.StartTime.Hour(), parentCourse.StartTime.Minute(), 0, 0,
					parentCourse.StartTime.Location(),
				)
				course.EndTime = course.StartTime.Add(time.Duration(course.Duration) * time.Minute)

				// Vérifier les conflits en excluant les cours de la même série récurrente
				conflicts, err := r.CheckConflictsExcluding(parentCourse.ID, &course)
				if err != nil {
					return err
				}
				if len(conflicts) > 0 {
					// Skip ce jour s'il y a un conflit
					continue
				}

				// Créer le cours
				if err := r.db.Create(&course).Error; err != nil {
					return err
				}
				break
			}
		}

		// Passer au jour suivant
		currentDate = currentDate.AddDate(0, 0, 1)
	}

	return nil
}

// GetFutureCoursesByUser récupère les cours futurs d'un utilisateur (enseignant)
func (r *CourseRepository) GetFutureCoursesByUser(userID uint) ([]models.Course, error) {
	var courses []models.Course
	now := time.Now()

	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("teacher_id = ? AND start_time > ?", userID, now).
		Find(&courses).Error

	return courses, err
}

// GetPastCoursesByUser récupère les cours passés d'un utilisateur (enseignant)
func (r *CourseRepository) GetPastCoursesByUser(userID uint) ([]models.Course, error) {
	var courses []models.Course
	now := time.Now()

	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("teacher_id = ? AND end_time < ?", userID, now).
		Find(&courses).Error

	return courses, err
}

// GetAllCoursesByUser récupère tous les cours d'un utilisateur (enseignant)
func (r *CourseRepository) GetAllCoursesByUser(userID uint) ([]models.Course, error) {
	var courses []models.Course

	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("teacher_id = ?", userID).
		Find(&courses).Error

	return courses, err
}

// GetFutureCoursesByRoom récupère les cours futurs d'une salle
func (r *CourseRepository) GetFutureCoursesByRoom(roomID uint) ([]models.Course, error) {
	var courses []models.Course
	now := time.Now()

	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("room_id = ? AND start_time > ?", roomID, now).
		Find(&courses).Error

	return courses, err
}

// GetCoursesBySubject récupère tous les cours d'une matière
func (r *CourseRepository) GetCoursesBySubject(subjectID uint) ([]models.Course, error) {
	var courses []models.Course

	err := r.db.Preload("Subject").Preload("Teacher").Preload("Room").
		Where("subject_id = ?", subjectID).
		Find(&courses).Error

	return courses, err
}

// HasAttendance vérifie si un cours a des présences enregistrées
func (r *CourseRepository) HasAttendance(courseID uint) (bool, error) {
	// Pour l'instant, on retourne false car la table des présences n'existe pas encore
	// TODO: Implémenter quand la table des présences sera créée
	// var count int64
	// err := r.db.Model(&models.Attendance{}).Where("course_id = ?", courseID).Count(&count).Error
	// return count > 0, err

	return false, nil
}
