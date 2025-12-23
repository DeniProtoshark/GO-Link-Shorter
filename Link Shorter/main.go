package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"math/rand"
	"time"
)

// Структура для хранения ссылки
type Link struct {
	OriginalURL string    `json:"original_url"`
	ShortCode   string    `json:"short_code"`
	CreatedAt   time.Time `json:"created_at"`
	IP          string    `json:"ip"`
	Visits      int       `json:"visits"`
}

// Структура для сортировки по посещениям
type LinkStats struct {
	ShortCode   string
	OriginalURL string
	Visits      int
	CreatedAt   time.Time
	IP          string
}

// Глобальные переменные
var (
	links      = make(map[string]*Link)    // short_code -> Link
	ipLinks    = make(map[string][]string) // ip -> []short_codes
	mutex      sync.RWMutex
	dbFile     = "data/links.json"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	
	// Создаем папку для базы данных
	os.MkdirAll("data", 0755)
	
	// Загружаем базу данных
	loadDatabase()
	
	// Стартовая страница
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Если это короткая ссылка - перенаправляем
		if r.URL.Path != "/" {
			shortCode := strings.TrimPrefix(r.URL.Path, "/")
			mutex.RLock()
			link, exists := links[shortCode]
			mutex.RUnlock()
			
			if exists {
				// Увеличиваем счетчик посещений
				mutex.Lock()
				link.Visits++
				mutex.Unlock()
				
				// Сохраняем изменения
				go func() {
					mutex.RLock()
					saveDatabase()
					mutex.RUnlock()
				}()
				
				http.Redirect(w, r, link.OriginalURL, http.StatusFound)
				return
			}
		}

		// Показываем форму
		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>🔗 Сократитель ссылок</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			max-width: 500px;
			margin: 50px auto;
			padding: 20px;
		}
		input {
			width: 100%%;
			padding: 10px;
			margin: 10px 0;
			font-size: 16px;
		}
		button {
			background: #0078d4;
			color: white;
			padding: 12px 24px;
			border: none;
			cursor: pointer;
			font-size: 16px;
		}
		button:hover {
			background: #005a9e;
		}
		.result {
			margin-top: 20px;
			padding: 15px;
			background: #e6f3ff;
			border-radius: 5px;
		}
		.menu {
			margin: 20px 0;
		}
		.menu a {
			margin-right: 15px;
			color: #0078d4;
			text-decoration: none;
		}
		.menu a:hover {
			text-decoration: underline;
		}
		.info {
			margin-top: 20px;
			padding: 15px;
			background: #f8f9fa;
			border-radius: 5px;
			font-size: 14px;
		}
		.domain {
			font-weight: bold;
			color: #28a745;
		}
		.badge {
			display: inline-block;
			padding: 3px 8px;
			border-radius: 10px;
			font-size: 12px;
			margin-left: 10px;
		}
		.badge-hot {
			background: #ff6b6b;
			color: white;
		}
		.badge-new {
			background: #4ecdc4;
			color: white;
		}
	</style>
</head>
<body>
	<div style="max-width: 600px; margin: 0 auto;">
		<h1>🔗 Сократитель ссылок</h1>
		
		<div class="menu">
			<a href="/">Главная</a>
			<a href="/my">Мои ссылки</a>
			<a href="/stats">Статистика</a>
		</div>
		
		<form method="POST" action="/shorten">
			<input type="url" name="url" placeholder="https://example.com" required>
			<button type="submit">Сократить</button>
		</form>
		
		<div class="info">
			<p><strong>Текущий домен:</strong> <span class="domain">%s</span></p>
			<p>Ссылки сохраняются автоматически в файл <code>%s</code></p>
		</div>
`, getCurrentDomain(r), dbFile)

		// Если есть результат от предыдущего запроса
		if result := r.URL.Query().Get("result"); result != "" {
			html += `<div class="result">
				<strong>Короткая ссылка:</strong><br>
				<a href="` + result + `">` + result + `</a><br>
				<small>Скопируйте эту ссылку</small>
			</div>`
		}

		html += `</div></body></html>`
		
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	// Создание короткой ссылки
	http.HandleFunc("/shorten", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		url := r.FormValue("url")
		if url == "" {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		// Добавляем протокол если нет
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "https://" + url
		}

		// Генерация короткого кода
		shortCode := generateCode(6)
		
		// Получаем IP пользователя
		ip := getIP(r)
		
		// Создаем запись
		link := &Link{
			OriginalURL: url,
			ShortCode:   shortCode,
			CreatedAt:   time.Now(),
			IP:          ip,
			Visits:      0,
		}
		
		// Сохраняем в память
		mutex.Lock()
		links[shortCode] = link
		ipLinks[ip] = append(ipLinks[ip], shortCode)
		mutex.Unlock()
		
		// Сохраняем в базу данных
		saveDatabase()

		// Показываем результат
		shortURL := getCurrentDomain(r) + "/" + shortCode
		http.Redirect(w, r, "/?result="+shortURL, http.StatusFound)
	})

	// Личный кабинет
	http.HandleFunc("/my", func(w http.ResponseWriter, r *http.Request) {
		ip := getIP(r)
		
		mutex.RLock()
		userCodes := ipLinks[ip]
		
		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>Мои ссылки</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			max-width: 800px;
			margin: 0 auto;
			padding: 20px;
		}
		.link {
			background: #f5f5f5;
			padding: 15px;
			margin: 10px 0;
			border-radius: 5px;
			border-left: 4px solid #0078d4;
		}
		.delete-btn {
			background: #dc3545;
			color: white;
			border: none;
			padding: 5px 10px;
			cursor: pointer;
			margin-top: 10px;
			border-radius: 3px;
		}
		.delete-btn:hover {
			background: #c82333;
		}
		.menu {
			margin: 20px 0;
		}
		.menu a {
			margin-right: 15px;
			color: #0078d4;
			text-decoration: none;
		}
		.menu a:hover {
			text-decoration: underline;
		}
		.no-links {
			padding: 20px;
			text-align: center;
			background: #f8f9fa;
			border-radius: 5px;
		}
		.url-info {
			font-size: 12px;
			color: #666;
			margin: 5px 0;
		}
		.short-url {
			font-family: monospace;
			font-size: 16px;
		}
		.info-box {
			background: #e8f4ff;
			padding: 15px;
			border-radius: 5px;
			margin: 20px 0;
		}
		.visits-count {
			display: inline-block;
			background: #28a745;
			color: white;
			padding: 2px 8px;
			border-radius: 10px;
			font-size: 12px;
			margin-left: 10px;
		}
		.badge {
			display: inline-block;
			padding: 3px 8px;
			border-radius: 10px;
			font-size: 12px;
			margin-left: 10px;
		}
		.badge-hot {
			background: #ff6b6b;
			color: white;
		}
	</style>
</head>
<body>
	<h1>👤 Мои ссылки</h1>
	
	<div class="menu">
		<a href="/">Главная</a>
		<a href="/my">Мои ссылки</a>
		<a href="/stats">Статистика</a>
	</div>
	
	<div class="info-box">
		<p><strong>Ваш IP:</strong> %s</p>
		<p><strong>Всего ссылок:</strong> %d</p>
	</div>
`, ip, len(userCodes))
		
		if len(userCodes) == 0 {
			html += `<div class="no-links">
				<p>У вас пока нет созданных ссылок</p>
				<a href="/">Создать первую ссылку</a>
			</div>`
		} else {
			// Сортируем ссылки пользователя по количеству посещений (убывание)
			userLinks := make([]LinkStats, 0, len(userCodes))
			for _, code := range userCodes {
				if link, exists := links[code]; exists {
					userLinks = append(userLinks, LinkStats{
						ShortCode:   code,
						OriginalURL: link.OriginalURL,
						Visits:      link.Visits,
						CreatedAt:   link.CreatedAt,
					})
				}
			}
			
			// Сортируем по убыванию количества посещений
			sort.Slice(userLinks, func(i, j int) bool {
				return userLinks[i].Visits > userLinks[j].Visits
			})
			
			for _, linkStat := range userLinks {
				shortURL := getCurrentDomain(r) + "/" + linkStat.ShortCode
				visitsBadge := ""
				if linkStat.Visits > 0 {
					visitsBadge = fmt.Sprintf(`<span class="visits-count">%d переходов</span>`, linkStat.Visits)
				}
				
				html += fmt.Sprintf(`
				<div class="link">
					<strong class="short-url"><a href="%s" target="_blank">%s</a>%s</strong>
					<div class="url-info">
						<strong>Оригинал:</strong> %s<br>
						<strong>Создано:</strong> %s
					</div>
					<a href="/delete/%s"><button class="delete-btn">Удалить</button></a>
				</div>`,
					shortURL, shortURL, visitsBadge,
					linkStat.OriginalURL,
					linkStat.CreatedAt.Format("02.01.2006 15:04"),
					linkStat.ShortCode)
			}
		}
		
		html += `</body></html>`
		
		mutex.RUnlock()
		
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	// Удаление ссылки
	http.HandleFunc("/delete/", func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/delete/")
		if code == "" {
			http.Redirect(w, r, "/my", http.StatusFound)
			return
		}
		
		ip := getIP(r)
		
		mutex.Lock()
		defer mutex.Unlock()
		
		// Проверяем, что ссылка существует и принадлежит этому IP
		if link, exists := links[code]; exists && link.IP == ip {
			// Удаляем ссылку
			delete(links, code)
			
			// Удаляем из списка ссылок пользователя
			if codes, ok := ipLinks[ip]; ok {
				newCodes := []string{}
				for _, c := range codes {
					if c != code {
						newCodes = append(newCodes, c)
					}
				}
				ipLinks[ip] = newCodes
			}
			
			// Сохраняем изменения
			saveDatabase()
			
			fmt.Printf("🗑️ Удалена ссылка: %s (IP: %s)\n", code, ip)
		}
		
		// Возвращаем в кабинет
		http.Redirect(w, r, "/my", http.StatusFound)
	})

	// Статистика
	http.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		mutex.RLock()
		defer mutex.RUnlock()

		totalLinks := len(links)
		totalVisits := 0
		for _, link := range links {
			totalVisits += link.Visits
		}
		
		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>Статистика</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			max-width: 800px;
			margin: 0 auto;
			padding: 20px;
		}
		.menu {
			margin: 20px 0;
		}
		.menu a {
			margin-right: 15px;
			color: #0078d4;
			text-decoration: none;
		}
		.menu a:hover {
			text-decoration: underline;
		}
		.stats-card {
			background: #f5f5f5;
			padding: 20px;
			border-radius: 5px;
			margin: 20px 0;
		}
		.link-item {
			padding: 10px;
			margin: 5px 0;
			background: white;
			border-radius: 3px;
			border-left: 3px solid #0078d4;
		}
		.stats-grid {
			display: grid;
			grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
			gap: 20px;
			margin: 20px 0;
		}
		.stat-box {
			background: #e8f4ff;
			padding: 15px;
			border-radius: 5px;
			text-align: center;
		}
		.stat-number {
			font-size: 32px;
			font-weight: bold;
			color: #0078d4;
		}
		.top-link {
			padding: 15px;
			margin: 10px 0;
			background: white;
			border-radius: 5px;
			border-left: 5px solid #ff6b6b;
		}
		.rank {
			display: inline-block;
			width: 30px;
			height: 30px;
			background: #0078d4;
			color: white;
			text-align: center;
			line-height: 30px;
			border-radius: 50%%;
			margin-right: 10px;
			font-weight: bold;
		}
		.rank-1 { background: #ffd700; }
		.rank-2 { background: #c0c0c0; }
		.rank-3 { background: #cd7f32; }
		.visits-badge {
			background: #28a745;
			color: white;
			padding: 3px 8px;
			border-radius: 10px;
			font-size: 12px;
			float: right;
		}
		.badge {
			display: inline-block;
			padding: 3px 8px;
			border-radius: 10px;
			font-size: 12px;
			margin-left: 10px;
		}
		.badge-hot {
			background: #ff6b6b;
			color: white;
		}
	</style>
</head>
<body>
	<h1>📊 Статистика</h1>
	
	<div class="menu">
		<a href="/">Главная</a>
		<a href="/my">Мои ссылки</a>
		<a href="/stats">Статистика</a>
	</div>
	
	<div class="stats-grid">
		<div class="stat-box">
			<div class="stat-number">%d</div>
			<div>Всего ссылок</div>
		</div>
		<div class="stat-box">
			<div class="stat-number">%d</div>
			<div>Всего переходов</div>
		</div>
		<div class="stat-box">
			<div class="stat-number">%d</div>
			<div>Уникальных IP</div>
		</div>
	</div>
	
	<div class="stats-card">
		<h3>Топ-5 самых популярных ссылок:</h3>
`, totalLinks, totalVisits, len(ipLinks))
		
		if len(links) == 0 {
			html += "<p>Ссылок пока нет</p>"
		} else {
			// Получаем топ-5 ссылок
			topLinks := getTopLinks(5)
			
			for i, linkStat := range topLinks {
				rankClass := ""
				if i == 0 {
					rankClass = "rank-1"
				} else if i == 1 {
					rankClass = "rank-2"
				} else if i == 2 {
					rankClass = "rank-3"
				}
				
				shortURL := getCurrentDomain(r) + "/" + linkStat.ShortCode
				html += fmt.Sprintf(`
				<div class="top-link">
					<div>
						<span class="rank %s">%d</span>
						<strong><a href="%s">%s</a></strong>
						<span class="visits-badge">%d переходов</span>
					</div>
					<div style="margin-left: 40px; margin-top: 10px; font-size: 14px; color: #666;">
						<strong>Оригинал:</strong> %s<br>
						<small>Создано: %s <!-- | IP: %s</small> -->
					</div>
				</div>`,
					rankClass, i+1,
					shortURL, shortURL, linkStat.Visits,
					linkStat.OriginalURL,
					linkStat.CreatedAt.Format("02.01.2006 15:04"),
					linkStat.IP)
			}
			
			html += `<p style="margin-top: 20px; text-align: center;">

			</p>`
		}
		
		html += `</div></body></html>`
		
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	// Топ ссылок (полная страница)
	http.HandleFunc("/top", func(w http.ResponseWriter, r *http.Request) {
		mutex.RLock()
		defer mutex.RUnlock()

		// Получаем топ-50 ссылок
		topLinks := getTopLinks(50)
		
		html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>Топ ссылок 🔥</title>
	<style>
		body {
			font-family: Arial, sans-serif;
			max-width: 900px;
			margin: 0 auto;
			padding: 20px;
		}
		.menu {
			margin: 20px 0;
		}
		.menu a {
			margin-right: 15px;
			color: #0078d4;
			text-decoration: none;
		}
		.menu a:hover {
			text-decoration: underline;
		}
		.top-link {
			padding: 15px;
			margin: 10px 0;
			background: white;
			border-radius: 5px;
			box-shadow: 0 2px 5px rgba(0,0,0,0.1);
			transition: transform 0.2s;
		}
		.top-link:hover {
			transform: translateY(-2px);
			box-shadow: 0 4px 10px rgba(0,0,0,0.15);
		}
		.rank {
			display: inline-block;
			width: 35px;
			height: 35px;
			background: #0078d4;
			color: white;
			text-align: center;
			line-height: 35px;
			border-radius: 50%%;
			margin-right: 15px;
			font-weight: bold;
			font-size: 16px;
		}
		.rank-1 { background: linear-gradient(135deg, #ffd700, #ffaa00); }
		.rank-2 { background: linear-gradient(135deg, #c0c0c0, #a0a0a0); }
		.rank-3 { background: linear-gradient(135deg, #cd7f32, #a65c00); }
		.visits-badge {
			background: #28a745;
			color: white;
			padding: 5px 12px;
			border-radius: 15px;
			font-size: 14px;
			float: right;
			font-weight: bold;
		}
		.url-info {
			margin-left: 50px;
			margin-top: 10px;
		}
		.short-url {
			font-family: monospace;
			font-size: 18px;
			font-weight: bold;
		}
		.original-url {
			color: #666;
			font-size: 14px;
			margin: 5px 0;
			word-break: break-all;
		}
		.meta-info {
			font-size: 12px;
			color: #888;
			margin-top: 8px;
		}
		.stats-header {
			background: linear-gradient(135deg, #ff6b6b, #ff8e53);
			color: white;
			padding: 20px;
			border-radius: 10px;
			margin: 20px 0;
			text-align: center;
		}
		.tabs {
			display: flex;
			margin: 20px 0;
			border-bottom: 2px solid #ddd;
		}
		.tab {
			padding: 10px 20px;
			cursor: pointer;
			border-bottom: 3px solid transparent;
		}
		.tab.active {
			border-bottom-color: #ff6b6b;
			font-weight: bold;
			color: #ff6b6b;
		}
		.filter {
			margin: 20px 0;
			padding: 15px;
			background: #f8f9fa;
			border-radius: 5px;
		}
		.filter select {
			padding: 8px;
			border-radius: 5px;
			border: 1px solid #ddd;
		}
		.empty-state {
			text-align: center;
			padding: 40px;
			color: #666;
		}
		.fire-icon {
			color: #ff6b6b;
			font-size: 24px;
			margin-right: 10px;
		}
		.badge {
			display: inline-block;
			padding: 3px 8px;
			border-radius: 10px;
			font-size: 12px;
			margin-left: 10px;
		}
		.badge-hot {
			background: #ff6b6b;
			color: white;
		}
	</style>
	<script>
		function filterTop(limit) {
			window.location.href = '/top?limit=' + limit;
		}
		
		// Автоматически обновляем страницу каждые 30 секунд
		setTimeout(function() {
			location.reload();
		}, 30000);
	</script>
</head>
<body>
	<h1><span class="fire-icon">🔥</span> Топ ссылок</h1>
	
	<div class="menu">
		<a href="/">Главная</a>
		<a href="/my">Мои ссылки</a>
		<a href="/stats">Статистика</a>
		<a href="/top">Топ ссылок <span class="badge badge-hot">🔥</span></a>
	</div>
	
	<div class="stats-header">
		<h2 style="margin: 0; color: white;">Самые популярные ссылки</h2>
		<p style="margin: 10px 0 0 0; opacity: 0.9;">Рейтинг основан на количестве переходов</p>
	</div>
	
	<div class="filter">
		<label for="limit">Показать топ:</label>
		<select id="limit" onchange="filterTop(this.value)">
			<option value="10" %s>10 ссылок</option>
			<option value="25" %s>25 ссылок</option>
			<option value="50" %s>50 ссылок</option>
			<option value="100" %s>100 ссылок</option>
			<option value="0" %s>Все ссылки</option>
		</select>
		<span style="margin-left: 20px; color: #666; font-size: 14px;">
			Страница обновится автоматически через 30 секунд
		</span>
	</div>
`, 
		getSelectedAttr("10", r),
		getSelectedAttr("25", r),
		getSelectedAttr("50", r),
		getSelectedAttr("100", r),
		getSelectedAttr("0", r))
		
		if len(topLinks) == 0 {
			html += `<div class="empty-state">
				<h3>Пока нет данных</h3>
				<p>Создайте первые ссылки, чтобы появился рейтинг</p>
				<a href="/">Создать ссылку</a>
			</div>`
		} else {
			// Определяем лимит из параметра запроса
			limit := 50
			if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
				fmt.Sscanf(limitParam, "%d", &limit)
				if limit <= 0 || limit > len(topLinks) {
					limit = len(topLinks)
				}
			}
			
			// Показываем только нужное количество
			if limit < len(topLinks) {
				topLinks = topLinks[:limit]
			}
			
			totalLinksCount := len(links)
			totalVisitsCount := 0
			for _, link := range links {
				totalVisitsCount += link.Visits
			}
			
			for i, linkStat := range topLinks {
				rankClass := ""
				if i == 0 {
					rankClass = "rank-1"
				} else if i == 1 {
					rankClass = "rank-2"
				} else if i == 2 {
					rankClass = "rank-3"
				}
				
				shortURL := getCurrentDomain(r) + "/" + linkStat.ShortCode
				
				// Определяем иконку активности
				activityIcon := "📈"
				if linkStat.Visits >= 100 {
					activityIcon = "🔥"
				} else if linkStat.Visits >= 50 {
					activityIcon = "🚀"
				} else if linkStat.Visits >= 10 {
					activityIcon = "⚡"
				}
				
				html += fmt.Sprintf(`
				<div class="top-link">
					<div>
						<span class="rank %s">%d</span>
						<span class="short-url"><a href="%s">%s</a></span>
						<span class="visits-badge">%s %d переходов</span>
					</div>
					<div class="url-info">
						<div class="original-url">%s</div>
						<div class="meta-info">
							Создано: %s <!| -- IP: %s -->
						</div>
					</div>
				</div>`,
					rankClass, i+1,
					shortURL, shortURL,
					activityIcon, linkStat.Visits,
					linkStat.OriginalURL,
					linkStat.CreatedAt.Format("02.01.2006 15:04"),
					linkStat.IP)
			}
			
			html += fmt.Sprintf(`
			<div style="margin-top: 30px; padding: 15px; background: #f8f9fa; border-radius: 5px; text-align: center;">
				<p>Показано <strong>%d</strong> из <strong>%d</strong> ссылок</p>
				<p>Всего переходов по всем ссылкам: <strong>%d</strong></p>
			</div>`, len(topLinks), totalLinksCount, totalVisitsCount)
		}
		
		html += `</body></html>`
		
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	})

	fmt.Println("========================================")
	fmt.Println("🚀 Сократитель ссылок запущен!")
	fmt.Println("📡 Порт: 8974")
	fmt.Println("👤 Кабинет: /my")
	fmt.Println("📊 Статистика: /stats")
	fmt.Println("💾 База данных:", dbFile)
	fmt.Println("========================================")
	
	// Запускаем автосохранение каждые 30 секунд
	go func() {
		for {
			time.Sleep(30 * time.Second)
			mutex.RLock()
			saveDatabase()
			mutex.RUnlock()
			fmt.Println("💾 Автосохранение базы данных...")
		}
	}()
	
	// Запускаем сервер
	err := http.ListenAndServe(":8974", nil)
	if err != nil {
		log.Fatal("Ошибка запуска сервера:", err)
	}
}

// Функция для получения выбранного атрибута в select
func getSelectedAttr(value string, r *http.Request) string {
	limitParam := r.URL.Query().Get("limit")
	if limitParam == "" {
		limitParam = "50" // Значение по умолчанию
	}
	
	if limitParam == value {
		return "selected"
	}
	return ""
}

// Получение текущего домена из запроса
func getCurrentDomain(r *http.Request) string {
	// Всегда используем HTTPS для коротких ссылок
	scheme := "https"
	host := r.Host
	
	// Если хост пустой (например, в тестах), используем localhost
	if host == "" {
		host = "localhost:8974"
		scheme = "http"
	}
	
	// Убираем порт если это стандартный HTTPS порт
	if strings.HasSuffix(host, ":443") {
		host = strings.TrimSuffix(host, ":443")
	}
	
	return scheme + "://" + host
}

// Получение IP адреса
func getIP(r *http.Request) string {
	// Пробуем получить из заголовка X-Forwarded-For (если за прокси)
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	
	// Если нет заголовка, берем RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// Если не удалось разделить (нет порта), возвращаем как есть
		return r.RemoteAddr
	}
	return ip
}

// Генерация случайного кода
func generateCode(length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

// Загрузка базы данных
func loadDatabase() {
	mutex.Lock()
	defer mutex.Unlock()
	
	absPath, _ := filepath.Abs(dbFile)
	fmt.Printf("📁 Загрузка базы данных: %s\n", absPath)
	
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		fmt.Println("📁 База данных не найдена, создаём новую")
		return
	}
	
	data, err := os.ReadFile(dbFile)
	if err != nil {
		fmt.Printf("❌ Ошибка чтения базы данных: %v\n", err)
		return
	}
	
	var loadedLinks []Link
	if err := json.Unmarshal(data, &loadedLinks); err != nil {
		fmt.Printf("❌ Ошибка парсинга базы данных: %v\n", err)
		return
	}
	
	// Восстанавливаем обе мапы
	links = make(map[string]*Link)
	ipLinks = make(map[string][]string)
	
	for i := range loadedLinks {
		link := &loadedLinks[i]
		links[link.ShortCode] = link
		ipLinks[link.IP] = append(ipLinks[link.IP], link.ShortCode)
	}
	
	fmt.Printf("✅ Загружено %d ссылок\n", len(loadedLinks))
}

// Сохранение базы данных
func saveDatabase() {
	mutex.RLock()
	defer mutex.RUnlock()
	
	var allLinks []Link
	for _, link := range links {
		allLinks = append(allLinks, *link)
	}
	
	data, err := json.MarshalIndent(allLinks, "", "  ")
	if err != nil {
		fmt.Printf("❌ Ошибка сериализации: %v\n", err)
		return
	}
	
	if err := os.WriteFile(dbFile, data, 0644); err != nil {
		fmt.Printf("❌ Ошибка записи файла: %v\n", err)
		return
	}
}

// Получение топ N ссылок по посещениям
func getTopLinks(n int) []LinkStats {
	var stats []LinkStats
	
	for code, link := range links {
		stats = append(stats, LinkStats{
			ShortCode:   code,
			OriginalURL: link.OriginalURL,
			Visits:      link.Visits,
			CreatedAt:   link.CreatedAt,
			IP:          link.IP,
		})
	}
	
	// Сортируем по убыванию количества посещений
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Visits == stats[j].Visits {
			// Если посещения равны, сортируем по дате создания (новые первыми)
			return stats[i].CreatedAt.After(stats[j].CreatedAt)
		}
		return stats[i].Visits > stats[j].Visits
	})
	
	// Возвращаем только N первых
	if n > 0 && n < len(stats) {
		return stats[:n]
	}
	
	return stats
}