insert into flights (
    id, flight_number, airline, origin, destination, departure_time,
    arrival_time, total_seats, available_seats, price_cents, status
)
values
    ('10000000-0000-0000-0000-000000000001', 'SU1234', 'Aeroflot', 'SVO', 'LED',
     '2026-04-01T08:00:00Z', '2026-04-01T09:30:00Z', 180, 180, 850000, 'SCHEDULED'),
    ('10000000-0000-0000-0000-000000000002', 'DP204', 'Pobeda', 'SVO', 'LED',
     '2026-04-01T12:00:00Z', '2026-04-01T13:35:00Z', 189, 3, 520000, 'SCHEDULED'),
    ('10000000-0000-0000-0000-000000000003', 'S71001', 'S7 Airlines', 'DME', 'AER',
     '2026-04-02T07:00:00Z', '2026-04-02T10:40:00Z', 160, 160, 1190000, 'SCHEDULED');

