package main

import (
	"math"
)

//CollisionPoint position for collision detect
type CollisionPoint struct {
	X float64
	Y float64
}

//CollisionCircle element for collision detect
type CollisionCircle struct {
	ID string
	CollisionPoint
}

//CollisionMap map for collision detect
type CollisionMap struct {
	circles       []CollisionCircle
	width         float64
	height        float64
	radius        float64
	staticObjects []CircleRoomObject
}

func (m *CollisionMap) tryMoveToPosition(circle CollisionCircle, nextPoint CollisionPoint) (CollisionPoint, bool) {
	backPoint := CollisionPoint{circle.X, circle.Y}
	backPointDist := GetDistance(backPoint.X, backPoint.Y, nextPoint.X, nextPoint.Y)

	safeSideXinit := math.Max(m.radius, math.Min(m.width-m.radius, nextPoint.X))
	safeSideYinit := math.Max(m.radius, math.Min(m.height-m.radius, nextPoint.Y))

	if safeSideXinit == nextPoint.X && safeSideYinit == nextPoint.Y {
		initPoint := CollisionPoint{nextPoint.X, nextPoint.Y}
		collisionCircle := m.getCollision(initPoint, m.circles)
		if collisionCircle == nil {
			return initPoint, false
		}
	}

	var searchDist float64 = 1.0
	i := 1

	angles := []float64{0, 0.78, 1.57, 2.35, 3.14, 3.92, 4.71, 5.49}

	for searchDist < backPointDist {
		for _, angle := range angles {
			sideX, sideY := m.getPointOnAngle(nextPoint, angle, searchDist)

			safeSideX := math.Max(m.radius, math.Min(m.width-m.radius, sideX))
			safeSideY := math.Max(m.radius, math.Min(m.height-m.radius, sideY))

			if safeSideX == sideX && safeSideY == sideY {
				sidePoint := CollisionPoint{sideX, sideY}
				collisionCircle := m.getCollision(sidePoint, m.circles)
				if collisionCircle == nil {
					return sidePoint, true
				}
			}

		}
		searchDist = searchDist + 1.0
		i++
	}

	return backPoint, true
}

func (m *CollisionMap) getPointOnAngle(from CollisionPoint, angle float64, radius float64) (float64, float64) {
	x := from.X + radius*math.Cos(angle)
	y := from.Y + radius*math.Sin(angle)

	return x, y
}

func (m *CollisionMap) getPointOnLine(from CollisionPoint, to CollisionPoint, distFull float64, dist float64) (float64, float64) {
	k := dist / distFull

	x := from.X + (to.X-from.X)*k
	y := from.Y + (to.Y-from.Y)*k

	return x, y
}

func (m *CollisionMap) getCollision(point CollisionPoint, circlesPool []CollisionCircle) *CollisionCircle {
	for _, staticObject := range m.staticObjects {
		if GetDistance(point.X, point.Y, staticObject.X, staticObject.Y) < m.radius+staticObject.Radius {
			return &CollisionCircle{
				ID: "0",
				CollisionPoint: CollisionPoint{
					X: staticObject.X,
					Y: staticObject.Y,
				},
			}
		}
	}

	for _, circle := range circlesPool {
		if GetDistance(point.X, point.Y, circle.CollisionPoint.X, circle.CollisionPoint.Y) < m.radius*2 {
			return &circle
		}
	}

	return nil
}

//GetPoint with collision detect
func GetPoint(currentClient *Client, staticObjects []CircleRoomObject, circles []CollisionCircle, radius float64, width float64, height float64, next CollisionPoint) (CollisionPoint, []Point) {
	fixRadius := radius + radius*0.03

	m := &CollisionMap{
		width:         width,
		height:        height,
		radius:        fixRadius,
		circles:       circles,
		staticObjects: staticObjects,
	}

	currentCircle := CollisionCircle{
		ID:             currentClient.ID,
		CollisionPoint: CollisionPoint{X: currentClient.PrevPositionX, Y: currentClient.PrevPositionY},
	}

	newNext, _ := m.tryMoveToPosition(currentCircle, next)
	pathDist := GetDistance(currentCircle.CollisionPoint.X, currentCircle.CollisionPoint.Y, newNext.X, newNext.Y)

	var points []CollisionPoint
	parts := 100.0
	localSpeed := bubleSpeed
	if pathDist < 4872 {
		localSpeed = bubleSpeed * 4872 / pathDist
	}

	if pathDist < 4872 && pathDist > 2436 {
		parts = 40.0
	}

	if pathDist < 2436 {
		parts = 20.0
	}

	interval := pathDist / parts
	num := int(math.Round(pathDist/interval)) - 1
	minDelay := pathDist * localSpeed / float64(num)

	multiPath := false
	if multiPath == true {
		intervalMult := 1
		if pathDist > interval*3 && len(m.staticObjects) > 0 {
			prevPoint := currentCircle.CollisionPoint

			i := 1
			for i < num {
				nPathDist := GetDistance(prevPoint.X, prevPoint.Y, newNext.X, newNext.Y)
				pathX, pathY := m.getPointOnLine(prevPoint, newNext, nPathDist, interval*float64(intervalMult))
				pointOnLine := CollisionPoint{X: pathX, Y: pathY}
				tmpCircle := CollisionCircle{
					ID:             currentClient.ID,
					CollisionPoint: CollisionPoint{X: prevPoint.X, Y: prevPoint.Y},
				}
				newMidNext, needPoint := m.tryMoveToPosition(tmpCircle, pointOnLine)
				if needPoint && prevPoint.X != newMidNext.X && prevPoint.Y != newMidNext.Y {
					point := CollisionPoint{
						X: newMidNext.X,
						Y: newMidNext.Y,
					}
					points = append(points, point)
					prevPoint = newMidNext
					intervalMult = 0
				}
				intervalMult++
				i++
			}
		}
	}
	point := CollisionPoint{
		X: newNext.X,
		Y: newNext.Y,
	}

	points = append(points, point)

	var pathPoints []Point

	for index, tPoint := range points {
		tPathDist := 0.0
		if index == 0 {
			tPathDist = GetDistance(currentCircle.CollisionPoint.X, currentCircle.CollisionPoint.Y, tPoint.X, tPoint.Y)
		} else {
			tPathDist = GetDistance(points[index-1].X, points[index-1].Y, tPoint.X, tPoint.Y)
		}

		tSpeedDelay := math.Max(float64(tPathDist*localSpeed), minDelay)

		if tPathDist < interval*2 {
			tSpeedDelay = tPathDist * bubleSpeed
		}

		pathPoint := Point{
			Index:    index,
			X:        Float64ToNumber(tPoint.X),
			Y:        Float64ToNumber(tPoint.Y),
			Duration: Float64ToNumber(tSpeedDelay),
		}
		pathPoints = append(pathPoints, pathPoint)
	}

	return newNext, pathPoints
}
