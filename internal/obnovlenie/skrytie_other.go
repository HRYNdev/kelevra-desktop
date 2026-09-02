//go:build !windows

package obnovlenie

// spryatatFayl вне Windows не делает ничего: атрибута «скрытый» тут нет, а
// точку в начало имени приписать нельзя — хвост обязан лежать ровно там же и
// ровно под тем именем, которое ищет UbratHvost и на которое откатывается
// bedaSOtkatom. Заглушка нужна, чтобы Postavit собиралась и проверялась на
// сервере без Windows (тот же приём, что у cmd/kelevra/okno_other.go).
func spryatatFayl(put string) error { return nil }
