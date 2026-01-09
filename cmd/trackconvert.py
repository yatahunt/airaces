import os
import csv
import shutil

def read_csv_without_comments(filepath):
    """
    Read CSV file, removing all comment lines starting with #.
    Returns rows as dictionaries and detected headers.
    """
    # Read all lines, skip comments
    with open(filepath, 'r') as f:
        data_lines = [line.strip() for line in f if line.strip() and not line.strip().startswith('#')]
    
    if not data_lines:
        raise ValueError("CSV file is empty")
    
    # Check if first line looks like a header or data
    first_line = data_lines[0].split(',')
    has_header = False
    
    try:
        float(first_line[0])
        has_header = False
    except ValueError:
        has_header = True
    
    # Parse CSV
    if has_header:
        reader = csv.DictReader(data_lines)
        rows = list(reader)
        input_headers = [h.strip() for h in reader.fieldnames]
    else:
        # No header, assume order: x_m, y_m, w_tr_right_m, w_tr_left_m
        rows = []
        input_headers = ['x_m', 'y_m', 'w_tr_right_m', 'w_tr_left_m']
        for line in data_lines:
            values = line.split(',')
            if len(values) != 4:
                raise ValueError(f"Expected 4 columns, found {len(values)}")
            rows.append({
                'x_m': values[0].strip(),
                'y_m': values[1].strip(),
                'w_tr_right_m': values[2].strip(),
                'w_tr_left_m': values[3].strip()
            })
    
    return rows, input_headers

def mirror_road_data(rows):
    """
    Create mirrored version of road data.
    - Calculates track length (max_x - min_x)
    - If length = 1789, mirrors as: new_x = 1789 + 50 - x
    - Otherwise, mirrors as: new_x = -x
    - Swaps left and right widths
    - Reverses row order to maintain direction
    """
    # Get all x values
    x_values = [float(row['x_m']) for row in rows]
    min_x = min(x_values)
    max_x = max(x_values)
    track_length = max_x - min_x
    
    print(f"  Track length (max_x - min_x): {track_length:.2f}")
    
    mirrored_rows = []
    for row in reversed(rows):
        x_original = float(row['x_m'])
        
        # Check if track length is 1789
        if abs(track_length - 1789) < 0.1:  # Allow small floating point error
            x_mirrored = 1789 + 50 - x_original
            print(f"  Using formula: 1789 + 50 - x")
        else:
            x_mirrored = -x_original
            print(f"  Using formula: -x")
        
        mirrored_row = {
            'x': str(x_mirrored),
            'y': row['y_m'],
            'wide_right': row['w_tr_left_m'],
            'wide_left': row['w_tr_right_m']
        }
        mirrored_rows.append(mirrored_row)
    
    return mirrored_rows

def write_csv_clean(filepath, rows, headers):
    """Write CSV file without any comments."""
    with open(filepath, 'w', newline='') as f:
        writer = csv.DictWriter(f, fieldnames=headers)
        writer.writeheader()
        writer.writerows(rows)

def process_all_csvs(input_dir='.', output_dir='./output'):
    """
    Process all CSV files:
    1. Copy original to output/xxx.mirror.csv (no comments, new headers)
    2. Create mirrored version as output/xxx.csv (no comments, new headers)
    """
    # Create output directory if it doesn't exist
    if not os.path.exists(output_dir):
        os.makedirs(output_dir)
        print(f"Created output directory: {output_dir}\n")
    
    # Get all CSV files in input directory
    csv_files = [f for f in os.listdir(input_dir) 
                 if f.endswith('.csv') and os.path.isfile(os.path.join(input_dir, f))]
    
    if not csv_files:
        print("No CSV files found to process.")
        return
    
    print(f"Found {len(csv_files)} CSV file(s) to process:\n")
    
    output_headers = ['x', 'y', 'wide_right', 'wide_left']
    
    for filename in csv_files:
        try:
            filepath = os.path.join(input_dir, filename)
            base_name = filename.rsplit('.csv', 1)[0]
            
            print(f"Processing: {filename}")
            
            # Read original data (without comments)
            original_rows, _ = read_csv_without_comments(filepath)
            
            # Convert original rows to new header format
            original_clean = []
            for row in original_rows:
                original_clean.append({
                    'x': row['x_m'],
                    'y': row['y_m'],
                    'wide_right': row['w_tr_right_m'],
                    'wide_left': row['w_tr_left_m']
                })
            
            # Create mirrored version
            mirrored_rows = mirror_road_data(original_rows)
            
            # Save original (cleaned) to output/xxx.mirror.csv
            mirror_filename = base_name + '.mirror.csv'
            mirror_path = os.path.join(output_dir, mirror_filename)
            write_csv_clean(mirror_path, original_clean, output_headers)
            print(f"  → Saved original to: {mirror_path}")
            
            # Save mirrored to output/xxx.csv
            output_filename = filename
            output_path = os.path.join(output_dir, output_filename)
            write_csv_clean(output_path, mirrored_rows, output_headers)
            print(f"  → Saved mirrored to: {output_path}")
            print()
            
        except Exception as e:
            print(f"  ✗ Error processing {filename}: {e}\n")

if __name__ == "__main__":
    # Process all CSV files from current directory to ./output
    process_all_csvs()
    print("Processing complete!")